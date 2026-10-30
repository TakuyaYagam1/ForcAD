//go:build windows

package operator

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

func assertPrivateMode(t *testing.T, path string, _ os.FileMode) os.FileInfo {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil || sd == nil {
		t.Fatalf("read private ACL: %v", err)
	}
	control, _, err := sd.Control()
	if err != nil || control&windows.SE_DACL_PROTECTED == 0 {
		t.Fatalf("ACL is not protected from inherited access: control=%x error=%v", control, err)
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{user.User.Sid.String(): true, "S-1-5-18": true, "S-1-5-32-544": true}
	dacl, _, err := sd.DACL()
	if err != nil || dacl == nil || int(dacl.AceCount) != len(want) {
		t.Fatalf("unexpected private ACL: %s, error=%v", sd.String(), err)
	}
	var flags byte
	if info.IsDir() {
		flags = windows.OBJECT_INHERIT_ACE | windows.CONTAINER_INHERIT_ACE
	}
	for index := uint32(0); index < uint32(dacl.AceCount); index++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(dacl, index, &ace); err != nil {
			t.Fatal(err)
		}
		sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart)).String()
		if !want[sid] || ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE || ace.Header.AceFlags != flags || ace.Mask != 0x1f01ff {
			t.Fatalf("unexpected ACL principal, inheritance or rights: %s", sd.String())
		}
		delete(want, sid)
	}
	return info
}

func broadlyReadableDirectory(t *testing.T) string {
	t.Helper()
	path := t.TempDir()
	// Only this synthetic test directory is made readable by Everyone. All file
	// contents below are test sentinels, never operator configuration or secrets.
	sd, err := windows.SecurityDescriptorFromString("D:P(A;OICI;FA;;;OW)(A;OICI;FR;;;WD)")
	if err != nil {
		t.Fatal(err)
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		t.Fatal(err)
	}
	if err := windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestWindowsPrivateFilesExcludeInheritedReadersBeforeWriting(t *testing.T) {
	directory := broadlyReadableDirectory(t)
	temporary, err := createPrivateTemp(directory)
	if err != nil {
		t.Fatal(err)
	}
	temporaryPath := temporary.Name()
	defer func() { _ = temporary.Close() }()
	if info := assertPrivateMode(t, temporaryPath, 0o600); info.Size() != 0 {
		t.Fatal("temporary file contains data before ACL verification")
	}
	if _, err := temporary.WriteString("synthetic-secret"); err != nil {
		t.Fatal(err)
	}
	if err := temporary.Close(); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(directory, "config.yml")
	if err := writePrivateFile(path, []byte("synthetic-config")); err != nil {
		t.Fatal(err)
	}
	assertPrivateMode(t, path, 0o600)
	backup, err := backupConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	assertPrivateMode(t, backup, 0o600)
	if data, err := os.ReadFile(backup); err != nil || string(data) != "synthetic-config" {
		t.Fatalf("private backup content changed: %v", err)
	}
	if err := writePrivateFile(path, []byte("replacement-sentinel")); err != nil {
		t.Fatal(err)
	}
	assertPrivateMode(t, path, 0o600)
}

func TestWindowsRuntimeDirectoryRemovesInheritedReaders(t *testing.T) {
	for _, existing := range []bool{false, true} {
		name := "new"
		if existing {
			name = "existing"
		}
		t.Run(name, func(t *testing.T) {
			parent := broadlyReadableDirectory(t)
			runtimePath := filepath.Join(parent, "runtime")
			if existing {
				if err := os.Mkdir(runtimePath, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			if err := ensureRuntimeDirectory(projectPaths{configDir: parent, runtime: runtimePath}); err != nil {
				t.Fatal(err)
			}
			assertPrivateMode(t, runtimePath, 0o700)
		})
	}
}

func TestWindowsPrivateCreationDoesNotReplaceExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "existing")
	if err := os.WriteFile(path, []byte("unchanged-sentinel"), 0o600); err != nil {
		t.Fatal(err)
	}
	if file, err := createPrivateFile(path); !errors.Is(err, os.ErrExist) || file != nil {
		t.Fatalf("exclusive creation = %v, %v", file, err)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "unchanged-sentinel" {
		t.Fatalf("exclusive creation modified existing data: %v", err)
	}
}

func TestWindowsPrivateVerificationRejectsBroadOrInheritableACL(t *testing.T) {
	expected, err := privateSecurityDescriptor(false)
	if err != nil {
		t.Fatal(err)
	}
	for _, sddl := range []string{"D:P(A;;FR;;;WD)", "D:(A;;FA;;;OW)"} {
		actual, err := windows.SecurityDescriptorFromString(sddl)
		if err != nil {
			t.Fatal(err)
		}
		if err := verifyPrivateDescriptor(actual, expected); err == nil {
			t.Fatal("unsafe ACL was accepted")
		}
	}
}
