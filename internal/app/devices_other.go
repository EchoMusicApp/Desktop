//go:build !darwin

package app

type AudioDevice struct {
	ID        uint32
	Name      string
	IsDefault bool
}

func getAudioOutputDevices() ([]AudioDevice, error) {
	return []AudioDevice{
		{ID: 1, Name: "Default Output", IsDefault: true},
	}, nil
}

func setAudioOutputDevice(id uint32) error {
	return nil
}
