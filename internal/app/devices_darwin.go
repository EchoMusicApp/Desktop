//go:build darwin

package app

import (
	"bytes"
	"fmt"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
)

type AudioDevice struct {
	ID        uint32
	Name      string
	IsDefault bool
}

type audioObjectPropertyAddress struct {
	Selector uint32
	Scope    uint32
	Element  uint32
}

const (
	kAudioObjectSystemObject                  = 1
	kAudioHardwarePropertyDevices             = 0x64657623 // 'dev#'
	kAudioHardwarePropertyDefaultOutputDevice = 0x644f7574 // 'dOut'
	kAudioObjectPropertyScopeGlobal           = 0x676c6f62 // 'glob'
	kAudioObjectPropertyScopeOutput           = 0x6f757470 // 'outp'
	kAudioDevicePropertyStreams               = 0x73746d23 // 'stm#'
	kAudioObjectPropertyName                  = 0x6e616d65 // 'name'
)

var (
	coreAudioOnce sync.Once
	coreAudioLib  uintptr
	fnGetPropSize func(uint32, *audioObjectPropertyAddress, uint32, unsafe.Pointer, *uint32) int32
	fnGetPropData func(uint32, *audioObjectPropertyAddress, uint32, unsafe.Pointer, *uint32, unsafe.Pointer) int32
	fnSetPropData func(uint32, *audioObjectPropertyAddress, uint32, unsafe.Pointer, uint32, unsafe.Pointer) int32
)

func initCoreAudio() error {
	var initErr error
	coreAudioOnce.Do(func() {
		lib, err := purego.Dlopen("/System/Library/Frameworks/CoreAudio.framework/CoreAudio", purego.RTLD_LAZY|purego.RTLD_GLOBAL)
		if err != nil {
			initErr = fmt.Errorf("coreaudio dlopen: %w", err)
			return
		}
		coreAudioLib = lib
		purego.RegisterLibFunc(&fnGetPropSize, lib, "AudioObjectGetPropertyDataSize")
		purego.RegisterLibFunc(&fnGetPropData, lib, "AudioObjectGetPropertyData")
		purego.RegisterLibFunc(&fnSetPropData, lib, "AudioObjectSetPropertyData")
	})
	return initErr
}

func getAudioOutputDevices() ([]AudioDevice, error) {
	if err := initCoreAudio(); err != nil {
		return nil, err
	}
	if fnGetPropData == nil || fnGetPropSize == nil {
		return nil, fmt.Errorf("coreaudio functions not available")
	}

	// 1. Get default output device ID
	defAddr := audioObjectPropertyAddress{
		Selector: kAudioHardwarePropertyDefaultOutputDevice,
		Scope:    kAudioObjectPropertyScopeGlobal,
		Element:  0,
	}
	var defaultDevID uint32
	size := uint32(unsafe.Sizeof(defaultDevID))
	if fnGetPropData(kAudioObjectSystemObject, &defAddr, 0, nil, &size, unsafe.Pointer(&defaultDevID)) != 0 {
		defaultDevID = 0
	}

	// 2. Get all audio devices
	devAddr := audioObjectPropertyAddress{
		Selector: kAudioHardwarePropertyDevices,
		Scope:    kAudioObjectPropertyScopeGlobal,
		Element:  0,
	}
	var dataSize uint32
	if fnGetPropSize(kAudioObjectSystemObject, &devAddr, 0, nil, &dataSize) != 0 || dataSize == 0 {
		return nil, fmt.Errorf("could not get devices size")
	}

	n := dataSize / 4
	devs := make([]uint32, n)
	if fnGetPropData(kAudioObjectSystemObject, &devAddr, 0, nil, &dataSize, unsafe.Pointer(&devs[0])) != 0 {
		return nil, fmt.Errorf("could not get devices data")
	}

	var result []AudioDevice
	for _, id := range devs {
		// Check if device has output streams
		var stmSize uint32
		stmAddr := audioObjectPropertyAddress{
			Selector: kAudioDevicePropertyStreams,
			Scope:    kAudioObjectPropertyScopeOutput,
			Element:  0,
		}
		if fnGetPropSize(id, &stmAddr, 0, nil, &stmSize) != 0 || stmSize == 0 {
			continue // No output streams
		}

		// Get device name
		nameAddr := audioObjectPropertyAddress{
			Selector: kAudioObjectPropertyName,
			Scope:    kAudioObjectPropertyScopeGlobal,
			Element:  0,
		}
		buf := make([]byte, 256)
		bufSize := uint32(len(buf))
		if fnGetPropData(id, &nameAddr, 0, nil, &bufSize, unsafe.Pointer(&buf[0])) != 0 {
			continue
		}
		name := string(bytes.TrimRight(buf[:bufSize], "\x00"))
		if name == "" {
			name = fmt.Sprintf("Audio Device %d", id)
		}

		result = append(result, AudioDevice{
			ID:        id,
			Name:      name,
			IsDefault: id == defaultDevID,
		})
	}

	return result, nil
}

func setAudioOutputDevice(id uint32) error {
	if err := initCoreAudio(); err != nil {
		return err
	}
	if fnSetPropData == nil {
		return fmt.Errorf("coreaudio function not available")
	}

	defAddr := audioObjectPropertyAddress{
		Selector: kAudioHardwarePropertyDefaultOutputDevice,
		Scope:    kAudioObjectPropertyScopeGlobal,
		Element:  0,
	}
	status := fnSetPropData(kAudioObjectSystemObject, &defAddr, 0, nil, uint32(unsafe.Sizeof(id)), unsafe.Pointer(&id))
	if status != 0 {
		return fmt.Errorf("AudioObjectSetPropertyData error: %d", status)
	}
	return nil
}
