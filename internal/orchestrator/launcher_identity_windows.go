//go:build windows

package orchestrator

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

// executablePathsMatch also recognizes signed launcher/main-image pairs. A
// matching name alone is never sufficient: the main image must be below the
// launcher's directory and both files must have the same trusted signing
// certificate. This works after a short-lived launcher has already exited.
func executablePathsMatch(actual, expected string) bool {
	actual, expected = normalizePath(actual), normalizePath(expected)
	if actual == "" || expected == "" {
		return false
	}
	if strings.EqualFold(actual, expected) {
		return true
	}
	if !launcherMainPath(actual, expected) {
		return false
	}
	// Junctions and symlinks must not extend the configured installation boundary.
	resolvedActual, err := filepath.EvalSymlinks(actual)
	if err != nil {
		return false
	}
	resolvedExpected, err := filepath.EvalSymlinks(expected)
	if err != nil || !launcherMainPath(resolvedActual, resolvedExpected) {
		return false
	}
	signer := cachedExecutableSigner(resolvedExpected)
	return signer != "" && signer == cachedExecutableSigner(resolvedActual)
}

func launcherMainPath(actual, expected string) bool {
	if !strings.EqualFold(filepath.Ext(expected), ".exe") || !strings.EqualFold(filepath.Base(actual), filepath.Base(expected)) {
		return false
	}
	dir := filepath.Dir(expected)
	// A drive/share root is not an installation directory.
	if filepath.Dir(dir) == dir {
		return false
	}
	rel, err := filepath.Rel(dir, filepath.Dir(actual))
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

type executableSignerEntry struct {
	info   os.FileInfo
	signer string
}

var executableSigners = struct {
	sync.Mutex
	files map[string]executableSignerEntry
}{files: make(map[string]executableSignerEntry)}

// Cache expensive trust checks, including refusals, only while the file's
// identity, size and modification time remain unchanged (updates replace it).
func cachedExecutableSigner(path string) string {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return ""
	}
	executableSigners.Lock()
	defer executableSigners.Unlock()
	old, found := executableSigners.files[path]
	if found && os.SameFile(old.info, info) && old.info.Size() == info.Size() && old.info.ModTime() == info.ModTime() {
		return old.signer
	}
	signer := trustedExecutableSigner(path)
	executableSigners.files[path] = executableSignerEntry{info: info, signer: signer}
	return signer
}

var (
	crypt32              = windows.NewLazySystemDLL("crypt32.dll")
	procCryptMsgGetParam = crypt32.NewProc("CryptMsgGetParam")
	procCryptMsgClose    = crypt32.NewProc("CryptMsgClose")
)

// trustedExecutableSigner verifies Authenticode before reading the primary
// signer's certificate. Offline verification keeps process/window scans from
// waiting for a revocation server; this is local app association, not an
// installer security check. Unsigned or unverifiable pairs remain exact-only.
func trustedExecutableSigner(path string) string {
	path16, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return ""
	}
	// Hold a read-only share across verification and certificate extraction so
	// an updater cannot replace or modify the image between the two operations.
	handle, err := windows.CreateFile(path16, windows.GENERIC_READ, windows.FILE_SHARE_READ, nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return ""
	}
	defer windows.CloseHandle(handle)
	file := windows.WinTrustFileInfo{Size: uint32(unsafe.Sizeof(windows.WinTrustFileInfo{})), FilePath: path16, File: handle}
	data := windows.WinTrustData{
		Size: uint32(unsafe.Sizeof(windows.WinTrustData{})), UIChoice: windows.WTD_UI_NONE,
		RevocationChecks: windows.WTD_REVOKE_NONE, UnionChoice: windows.WTD_CHOICE_FILE,
		StateAction: windows.WTD_STATEACTION_VERIFY, ProvFlags: windows.WTD_CACHE_ONLY_URL_RETRIEVAL,
		FileOrCatalogOrBlobOrSgnrOrCert: unsafe.Pointer(&file),
	}
	verifyErr := windows.WinVerifyTrustEx(windows.InvalidHWND, &windows.WINTRUST_ACTION_GENERIC_VERIFY_V2, &data)
	data.StateAction = windows.WTD_STATEACTION_CLOSE
	closeErr := windows.WinVerifyTrustEx(windows.InvalidHWND, &windows.WINTRUST_ACTION_GENERIC_VERIFY_V2, &data)
	if verifyErr != nil || closeErr != nil {
		return ""
	}
	var store, message windows.Handle
	var encoding uint32
	if windows.CryptQueryObject(windows.CERT_QUERY_OBJECT_FILE, unsafe.Pointer(path16),
		windows.CERT_QUERY_CONTENT_FLAG_PKCS7_SIGNED_EMBED, windows.CERT_QUERY_FORMAT_FLAG_BINARY,
		0, &encoding, nil, nil, &store, &message, nil) != nil {
		return ""
	}
	defer windows.CertCloseStore(store, 0)
	defer procCryptMsgClose.Call(uintptr(message))
	const signerCertInfo = 7 // CMSG_SIGNER_CERT_INFO_PARAM, primary signer index 0
	var size uint32
	if ok, _, _ := procCryptMsgGetParam.Call(uintptr(message), signerCertInfo, 0, 0, uintptr(unsafe.Pointer(&size))); ok == 0 || size < uint32(unsafe.Sizeof(windows.CertInfo{})) {
		return ""
	}
	buf := make([]byte, size)
	if ok, _, _ := procCryptMsgGetParam.Call(uintptr(message), signerCertInfo, 0, uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size))); ok == 0 {
		return ""
	}
	cert, err := windows.CertFindCertificateInStore(store, encoding, 0, windows.CERT_FIND_SUBJECT_CERT, unsafe.Pointer(&buf[0]), nil)
	runtime.KeepAlive(buf)
	if err != nil {
		return ""
	}
	defer windows.CertFreeCertificateContext(cert)
	return string(unsafe.Slice(cert.EncodedCert, cert.Length))
}
