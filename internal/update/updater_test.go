package update

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStageUpdateBinaryCopiesNewExecutableIntoTargetDirectory(t *testing.T) {
	targetDir := t.TempDir()
	targetPath := filepath.Join(targetDir, "BizantiAgent.exe")
	sourcePath := filepath.Join(t.TempDir(), "BizantiAgent-new.exe")

	if err := os.WriteFile(sourcePath, []byte("new-binary"), 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}

	stagedPath, err := stageUpdateBinary(targetPath, sourcePath)
	if err != nil {
		t.Fatalf("stage update binary: %v", err)
	}

	expectedPath := filepath.Join(targetDir, "BizantiAgent.update.exe")
	if stagedPath != expectedPath {
		t.Fatalf("unexpected staged path: got %q want %q", stagedPath, expectedPath)
	}

	content, err := os.ReadFile(stagedPath)
	if err != nil {
		t.Fatalf("read staged file: %v", err)
	}

	if string(content) != "new-binary" {
		t.Fatalf("unexpected staged file content: %q", string(content))
	}
}

func TestBuildWindowsUpdateScriptEscapesPathsAndLogsFailures(t *testing.T) {
	script := buildWindowsUpdateScript(
		`C:\Temp\O'Hara\BizantiAgent.exe`,
		`C:\Temp\O'Hara\BizantiAgent.update.exe`,
		`C:\Users\adam\AppData\Local\Temp\new'binary.exe`,
		`C:\ProgramData\BizantiAgent\logs\update.log`,
	)

	assertContains := func(fragment string) {
		t.Helper()
		if !strings.Contains(script, fragment) {
			t.Fatalf("script does not contain %q\n%s", fragment, script)
		}
	}

	assertContains(`$target = 'C:\Temp\O''Hara\BizantiAgent.exe'`)
	assertContains(`$staged = 'C:\Temp\O''Hara\BizantiAgent.update.exe'`)
	assertContains(`$downloaded = 'C:\Users\adam\AppData\Local\Temp\new''binary.exe'`)
	assertContains(`$logPath = 'C:\ProgramData\BizantiAgent\logs\update.log'`)
	assertContains(`BizantiAgent.previous.exe`)
	assertContains(`Write-UpdateLog "Próba #$attempt nieudana: $($_.Exception.Message)"`)
	assertContains(`Start-Process -FilePath $target | Out-Null`)
}
