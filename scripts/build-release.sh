#!/bin/bash
set -e

REPO_DIR="/var/www/bizanti-dev-modules/BizantiAgent"
cd "$REPO_DIR"

echo "=== BizantiAgent Automated Release Builder ==="
echo ""

# 0. Locate a Go toolchain (not always on PATH in this environment)
if ! command -v go >/dev/null 2>&1; then
    for candidate in /usr/local/go/bin /usr/lib/go/bin; do
        if [ -x "$candidate/go" ]; then
            export PATH="$candidate:$PATH"
            break
        fi
    done
fi

if ! command -v go >/dev/null 2>&1; then
    echo "❌ ERROR: 'go' toolchain not found on PATH (checked /usr/local/go/bin)."
    echo "   Install Go or adjust this script's PATH lookup."
    exit 1
fi

# 0b. Locate go-winres (embeds icon + VERSIONINFO + manifest into the .syso).
# Installed into GOPATH/bin; add it to PATH and install on first run if missing.
export PATH="$PATH:$(go env GOPATH)/bin"
if ! command -v go-winres >/dev/null 2>&1; then
    echo "go-winres not found — installing..."
    go install github.com/tc-hib/go-winres@latest
fi

# 1. Read current version
CURRENT_VERSION=$(grep 'Version = ' internal/version/version.go | sed 's/.*Version = "\([^"]*\)".*/\1/')
echo "[1/7] Current version: $CURRENT_VERSION"

# Parse version components
MAJOR=$(echo "$CURRENT_VERSION" | cut -d. -f1)
MINOR=$(echo "$CURRENT_VERSION" | cut -d. -f2)
PATCH=$(echo "$CURRENT_VERSION" | cut -d. -f3)

# Increment patch version
NEW_PATCH=$((PATCH + 1))
NEW_VERSION="$MAJOR.$MINOR.$NEW_PATCH"
NEW_TAG="v$NEW_VERSION"

echo "[2/7] New version: $NEW_VERSION"
echo ""

# 2. Update version file
echo "[3/7] Updating version file..."
sed -i "s/Version = \"$CURRENT_VERSION\"/Version = \"$NEW_VERSION\"/" internal/version/version.go
git add internal/version/version.go
git commit -m "Bump version to $NEW_VERSION

Co-Authored-By: Claude Haiku 4.5 <noreply@anthropic.com>"

echo "✓ Version bumped"
echo ""

# 3. Rebuild the Windows binary with the bumped version embedded.
# Must happen AFTER the version bump above — building before it would embed
# the old version string into a binary published under the new tag.
echo "[4/7] Rebuilding BizantiAgent.exe (GOOS=windows) with version $NEW_VERSION..."

# 3a. Sync the 4-part version into winres.json and regenerate the resource .syso,
# so the embedded VERSIONINFO (file/product version) matches the bumped version.
# The only 4-part numbers in winres.json are the version fields, so a blanket replace is safe.
sed -i -E "s/[0-9]+\.[0-9]+\.[0-9]+\.0/${NEW_VERSION}.0/g" winres/winres.json
go-winres make --in winres/winres.json --arch amd64 --out cmd/bizanti-agent/rsrc

# -s strips the symbol table; -w (DWARF strip) intentionally omitted — a fully
# stripped binary raises the antivirus ML "packed" score.
GOOS=windows GOARCH=amd64 go build -ldflags "-H=windowsgui -s" -o BizantiAgent.exe ./cmd/bizanti-agent

if [ ! -f "BizantiAgent.exe" ]; then
    echo "❌ ERROR: build did not produce BizantiAgent.exe"
    exit 1
fi

if ! strings BizantiAgent.exe 2>/dev/null | grep -q "$NEW_VERSION"; then
    echo "❌ ERROR: rebuilt binary does not contain version string $NEW_VERSION — aborting before publishing a stale build."
    exit 1
fi

BINARY_SIZE=$(ls -lh BizantiAgent.exe | awk '{print $5}')
echo "✓ Binary built and verified: BizantiAgent.exe ($BINARY_SIZE, embeds $NEW_VERSION)"

git add BizantiAgent.exe winres/winres.json cmd/bizanti-agent/rsrc_windows_amd64.syso
git commit -m "Rebuild BizantiAgent.exe for $NEW_TAG

Co-Authored-By: Claude Haiku 4.5 <noreply@anthropic.com>"

# Copy to releases folder
mkdir -p "releases/bizanti-agent-$NEW_TAG-win64"
cp BizantiAgent.exe "releases/bizanti-agent-$NEW_TAG-win64/"
echo "✓ Binary copied to releases/"
echo ""

# 4. Create git tag
echo "[5/7] Creating git tag..."
git tag -a "$NEW_TAG" -m "Release $NEW_TAG"
git push origin main
git push origin "$NEW_TAG"
echo "✓ Tag created and pushed"
echo ""

# 5. Create GitHub Release and upload binary
echo "[6/7] Creating GitHub Release..."
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
