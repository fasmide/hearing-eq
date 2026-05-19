package pasink

import (
	"fmt"

	control "github.com/the-jonsey/pulseaudio"
)

const (
	SinkName        = "hearing-eq"
	SinkDescription = "Hearing-EQ"
	moduleName      = "module-null-sink"
)

type Sink struct {
	client      *control.Client
	moduleIndex uint32
	name        string
}

func Create(client *control.Client) (*Sink, error) {
	args := fmt.Sprintf("sink_name=%s sink_properties=device.description=%s media.class=Audio/Sink", SinkName, SinkDescription)
	idx, err := client.LoadModule(moduleName, args)
	if err != nil {
		return nil, fmt.Errorf("load %s: %w", moduleName, err)
	}
	return &Sink{client: client, moduleIndex: idx, name: SinkName}, nil
}

func (s *Sink) ModuleIndex() uint32 {
	return s.moduleIndex
}

func (s *Sink) Name() string {
	return s.name
}

func (s *Sink) Close() error {
	if s == nil || s.client == nil {
		return nil
	}
	if err := s.client.UnloadModule(s.moduleIndex); err != nil {
		return fmt.Errorf("unload virtual sink module %d: %w", s.moduleIndex, err)
	}
	s.client = nil
	return nil
}
