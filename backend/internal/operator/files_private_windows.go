//go:build windows

package operator

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Secrets are accessible to the creating user, SYSTEM and administrators only.
// Protection is applied at creation, before an inherited reader could open the
// empty file and retain access while secret data is written to it.
func privateSecurityDescriptor(directory bool) (*windows.SECURITY_DESCRIPTOR, error) {
	const location = "operator.privateSecurityDescriptor"
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, fmt.Errorf("%s - read process user: %w", location, err)
	}
	flags := ""
	if directory {
		flags = "OICI"
	}
	var sddl strings.Builder
	sddl.WriteString("D:P")
	seen := make(map[string]bool)
	for _, sid := range []string{user.User.Sid.String(), "S-1-5-18", "S-1-5-32-544"} {
		if !seen[sid] {
			fmt.Fprintf(&sddl, "(A;%s;FA;;;%s)", flags, sid)
			seen[sid] = true
		}
	}
	descriptor, err := windows.SecurityDescriptorFromString(sddl.String())
	if err != nil {
		return nil, fmt.Errorf("%s - construct private security descriptor: %w", location, err)
	}
	return descriptor, nil
}

func createPrivateFile(path string) (*os.File, error) {
	const location = "operator.createPrivateFile"
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, fmt.Errorf("%s - %w", location, err)
	}
	sd, err := privateSecurityDescriptor(false)
	if err != nil {
		return nil, fmt.Errorf("%s - %w", location, err)
	}
	attributes := windows.SecurityAttributes{
		Length:             uint32(unsafe.Sizeof(windows.SecurityAttributes{})),
		SecurityDescriptor: sd,
	}
	handle, err := windows.CreateFile(name, windows.GENERIC_READ|windows.GENERIC_WRITE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		&attributes, windows.CREATE_NEW, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return nil, fmt.Errorf("%s - create private file: %w", location, &os.PathError{
			Op:   "create",
			Path: path,
			Err:  err,
		})
	}
	if err := verifyPrivateHandle(handle, sd); err != nil {
		_ = windows.CloseHandle(handle)
		_ = os.Remove(path)
		return nil, fmt.Errorf("%s - verify file ACL before writing: %w", location, err)
	}
	return os.NewFile(uintptr(handle), path), nil
}

func ensurePrivateDirectory(path string) error {
	const location = "operator.ensurePrivateDirectory"
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return fmt.Errorf("%s - %w", location, err)
	}
	sd, err := privateSecurityDescriptor(true)
	if err != nil {
		return fmt.Errorf("%s - %w", location, err)
	}
	attributes := windows.SecurityAttributes{
		Length:             uint32(unsafe.Sizeof(windows.SecurityAttributes{})),
		SecurityDescriptor: sd,
	}
	if err := windows.CreateDirectory(name, &attributes); err != nil && !errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		return fmt.Errorf("%s - create directory: %w", location, err)
	}
	handle, err := windows.CreateFile(name, windows.READ_CONTROL|windows.WRITE_DAC|windows.FILE_READ_ATTRIBUTES,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return fmt.Errorf("%s - open directory security: %w", location, err)
	}
	defer func() {
		_ = windows.CloseHandle(handle)
	}()
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(handle, &info); err != nil {
		return fmt.Errorf("%s - inspect directory handle: %w", location, err)
	}
	if info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY == 0 || info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return fmt.Errorf("%s - path must be a regular directory", location)
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		return fmt.Errorf("%s - %w", location, err)
	}
	if err := windows.SetSecurityInfo(handle, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil); err != nil {
		return fmt.Errorf("%s - protect directory ACL: %w", location, err)
	}
	if err := verifyPrivateHandle(handle, sd); err != nil {
		return fmt.Errorf("%s - verify protected directory: %w", location, err)
	}
	return nil
}

func verifyPrivateHandle(handle windows.Handle, expected *windows.SECURITY_DESCRIPTOR) error {
	const location = "operator.verifyPrivateHandle"

	actual, err := windows.GetSecurityInfo(handle, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return fmt.Errorf("%s - %w", location, err)
	}
	if err := verifyPrivateDescriptor(actual, expected); err != nil {
		return fmt.Errorf("%s - verify private security descriptor: %w", location, err)
	}
	return nil
}

func verifyPrivateDescriptor(actual, expected *windows.SECURITY_DESCRIPTOR) error {
	const location = "operator.verifyPrivateDescriptor"
	if actual == nil {
		return fmt.Errorf("%s - filesystem did not retain a security descriptor", location)
	}
	control, _, err := actual.Control()
	if err != nil {
		return fmt.Errorf("%s - %w", location, err)
	}
	if control&windows.SE_DACL_PROTECTED == 0 {
		return fmt.Errorf("%s - ACL inheritance is not disabled", location)
	}
	got, err := privateACLEntries(actual)
	if err != nil {
		return fmt.Errorf("%s - %w", location, err)
	}
	want, err := privateACLEntries(expected)
	if err != nil {
		return fmt.Errorf("%s - %w", location, err)
	}
	if len(got) != len(want) {
		return fmt.Errorf("%s - unexpected ACL entry count", location)
	}
	for entry, count := range want {
		if got[entry] != count {
			return fmt.Errorf("%s - unexpected ACL principal or access rights", location)
		}
	}
	return nil
}

func privateACLEntries(sd *windows.SECURITY_DESCRIPTOR) (map[string]int, error) {
	const location = "operator.privateACLEntries"

	dacl, _, err := sd.DACL()
	if err != nil {
		return nil, fmt.Errorf("%s - %w", location, err)
	}
	if dacl == nil {
		return nil, fmt.Errorf("%s - null ACL permits unrestricted access", location)
	}
	entries := make(map[string]int, dacl.AceCount)
	for index := uint32(0); index < uint32(dacl.AceCount); index++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(dacl, index, &ace); err != nil {
			return nil, fmt.Errorf("%s - %w", location, err)
		}
		if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE {
			return nil, fmt.Errorf("%s - unexpected ACL entry type", location)
		}
		// #nosec G103 -- GetAce returns a Windows ACCESS_ALLOWED_ACE; SidStart begins its embedded SID.
		sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
		entries[fmt.Sprintf("%s:%x:%x", sid.String(), ace.Mask, ace.Header.AceFlags)]++
	}
	return entries, nil
}
