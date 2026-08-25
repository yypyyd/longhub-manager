//go:build windows

package manageragent

import (
	"errors"
	"runtime"
	"syscall"
	"unsafe"
)

const (
	credentialTypeGeneric         = 1
	credentialPersistLocalMachine = 2
	errorNotFound                 = syscall.Errno(1168)
)

var (
	advapi32        = syscall.NewLazyDLL("advapi32.dll")
	procCredWriteW  = advapi32.NewProc("CredWriteW")
	procCredReadW   = advapi32.NewProc("CredReadW")
	procCredDeleteW = advapi32.NewProc("CredDeleteW")
	procCredFree    = advapi32.NewProc("CredFree")
)

type windowsCredential struct {
	Flags              uint32
	Type               uint32
	TargetName         *uint16
	Comment            *uint16
	LastWritten        syscall.Filetime
	CredentialBlobSize uint32
	CredentialBlob     *byte
	Persist            uint32
	AttributeCount     uint32
	Attributes         uintptr
	TargetAlias        *uint16
	UserName           *uint16
}

type platformSecretStore struct{ target string }

func NewPlatformSecretStore(_ string) SecretStore {
	return &platformSecretStore{target: "LongHub/ManagerAgent/APIKey"}
}

func (s *platformSecretStore) Get() (string, error) {
	target, err := syscall.UTF16PtrFromString(s.target)
	if err != nil {
		return "", err
	}
	var credential *windowsCredential
	r1, _, callErr := procCredReadW.Call(
		uintptr(unsafe.Pointer(target)), credentialTypeGeneric, 0,
		uintptr(unsafe.Pointer(&credential)),
	)
	runtime.KeepAlive(target)
	if r1 == 0 {
		if errors.Is(callErr, errorNotFound) {
			return "", nil
		}
		return "", callErr
	}
	defer procCredFree.Call(uintptr(unsafe.Pointer(credential)))
	if credential == nil || credential.CredentialBlobSize == 0 {
		return "", nil
	}
	if credential.CredentialBlob == nil || credential.CredentialBlobSize > 2048 {
		return "", errors.New("credential blob is unavailable")
	}
	data := unsafe.Slice(credential.CredentialBlob, int(credential.CredentialBlobSize))
	return string(append([]byte(nil), data...)), nil
}

func (s *platformSecretStore) Set(value string) error {
	if value == "" || len(value) > 2048 {
		return errors.New("credential value is invalid")
	}
	target, err := syscall.UTF16PtrFromString(s.target)
	if err != nil {
		return err
	}
	user, err := syscall.UTF16PtrFromString("LongHub Manager")
	if err != nil {
		return err
	}
	data := []byte(value)
	credential := windowsCredential{
		Type:               credentialTypeGeneric,
		TargetName:         target,
		CredentialBlobSize: uint32(len(data)),
		CredentialBlob:     &data[0],
		Persist:            credentialPersistLocalMachine,
		UserName:           user,
	}
	r1, _, callErr := procCredWriteW.Call(uintptr(unsafe.Pointer(&credential)), 0)
	runtime.KeepAlive(target)
	runtime.KeepAlive(user)
	runtime.KeepAlive(data)
	if r1 == 0 {
		return callErr
	}
	return nil
}

func (s *platformSecretStore) Delete() error {
	target, err := syscall.UTF16PtrFromString(s.target)
	if err != nil {
		return err
	}
	r1, _, callErr := procCredDeleteW.Call(uintptr(unsafe.Pointer(target)), credentialTypeGeneric, 0)
	runtime.KeepAlive(target)
	if r1 == 0 && !errors.Is(callErr, errorNotFound) {
		return callErr
	}
	return nil
}
