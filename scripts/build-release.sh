#!/bin/bash
set -e

REPO_DIR="/var/www/bizanti-dev-modules/BizantiAgent"
cd "$REPO_DIR"

echo "=== BizantiAgent Automated Release Builder ==="
echo ""

# 1. Read current version
CURRENT_VERSION=$(grep 'Version = ' internal/version/version.go | sed 's/.*Version = "\([^"]*\)".*/\1/')
echo "[1/6] Current version: $CURRENT_VERSION"

# Parse version components
MAJOR=$(echo "$CURRENT_VERSION" | cut -d. -f1)
MINOR=$(echo "$CURRENT_VERSION" | cut -d. -f2)
PATCH=$(echo "$CURRENT_VERSION" | cut -d. -f3)

# Increment patch version
NEW_PATCH=$((PATCH + 1))
NEW_VERSION="$MAJOR.$MINOR.$NEW_PATCH"
NEW_TAG="v$NEW_VERSION"

echo "[2/6] New version: $NEW_VERSION"
echo ""

# 2. Update version file
echo "[3/6] Updating version file..."
sed -i "s/Version = \"$CURRENT_VERSION\"/Version = \"$NEW_VERSION\"/" internal/version/version.go
git add internal/version/version.go
git commit -m "Bump version to $NEW_VERSION

Co-Authored-By: Claude Haiku 4.5 <noreply@anthropic.com>"

echo "✓ Version bumped"
echo ""

# 3. Check for Windows binary
echo "[4/6] Looking for Windows binary..."
if [ ! -f "BizantiAgent.exe" ]; then
    echo "❌ ERROR: BizantiAgent.exe not found!"
    echo "   On Windows, run: .\scripts\build-windows.ps1"
    echo "   Then on Linux, run this script again."
    exit 1
fi

BINARY_SIZE=$(ls -lh BizantiAgent.exe | awk '{print $5}')
echo "✓ Binary found: BizantiAgent.exe ($BINARY_SIZE)"

# Copy to releases folder
mkdir -p "releases/bizanti-agent-$NEW_TAG-win64"
cp BizantiAgent.exe "releases/bizanti-agent-$NEW_TAG-win64/"
echo "✓ Binary copied to releases/"
echo ""

# 4. Create git tag
echo "[5/6] Creating git tag..."
git tag -a "$NEW_TAG" -m "Release $NEW_TAG"
git push origin main
git push origin "$NEW_TAG"
echo "✓ Tag created and pushed"
echo ""

# 5. Create GitHub Release and upload binary
echo "[6/6] Creating GitHub Release..."
TOKEN=$(cat ~/.git-credentials | grep github.com | sed 's|https://[^:]*:\([^@]*\)@github.com|\1|')

if [ -z "$TOKEN" ]; then
    echo "❌ ERROR: Could not extract GitHub token from ~/.git-credentials"
    exit 1
fi

# Create release
RELEASE_RESPONSE=$(curl -s -X POST \
  -H "Authorization: token $TOKEN" \
  -H "Accept: application/vnd.github+json" \
  "https://api.github.com/repos/NowakAdmin/BizantiAgent/releases" \
  -d "{
    \"tag_name\": \"$NEW_TAG\",
    \"name\": \"Release $NEW_TAG\",
    \"body\": \"## Changes\\n\\nSee commit log: https://github.com/NowakAdmin/BizantiAgent/compare/v$CURRENT_VERSION...$NEW_TAG\",
    \"draft\": false,
    \"prerelease\": false
  }")

RELEASE_ID=$(echo "$RELEASE_RESPONSE" | python3 -c "import sys,json; r=json.load(sys.stdin); print(r.get('id',''))" 2>/dev/null || echo "")

if [ -z "$RELEASE_ID" ]; then
    echo "❌ ERROR: Failed to create GitHub release"
    echo "Response: $RELEASE_RESPONSE"
    exit 1
fi

echo "✓ Release created (ID: $RELEASE_ID)"

# Upload binary
echo "  Uploading binary..."
UPLOAD_RESPONSE=$(curl -s -X POST \
  -H "Authorization: token $TOKEN" \
  -H "Accept: application/vnd.github+json" \
  -H "Content-Type: application/octet-stream" \
  "https://uploads.github.com/repos/NowakAdmin/BizantiAgent/releases/$RELEASE_ID/assets?name=BizantiAgent-$NEW_TAG.exe" \
  --data-binary @"releases/bizanti-agent-$NEW_TAG-win64/BizantiAgent.exe")

ASSET_NAME=$(echo "$UPLOAD_RESPONSE" | python3 -c "import sys,json; r=json.load(sys.stdin); print(r.get('name',''))" 2>/dev/null || echo "")

if [ -z "$ASSET_NAME" ]; then
    echo "❌ ERROR: Failed to upload binary"
    echo "Response: $UPLOAD_RESPONSE"
    exit 1
fi

echo "✓ Binary uploaded: $ASSET_NAME"
echo ""

echo "=== Release Complete ==="
echo "Release: $NEW_TAG"
echo "Binary: https://github.com/NowakAdmin/BizantiAgent/releases/download/$NEW_TAG/$ASSET_NAME"
echo ""
