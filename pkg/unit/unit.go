package unit

import (
	"fmt"
	"time"

	"github.com/coreos/go-systemd/v22/dbus"
)

type Unit struct {
	Name          string        `json:"name"`
	Description   string        `json:"description,omitempty"`
	ActiveState   string        `json:"active_state"`
	SubState      string        `json:"sub_state"`
	LoadState     string        `json:"load_state"`
	PID           int           `json:"pid,omitempty"`
	CPUUsage      float64       `json:"cpu_seconds,omitempty"`
	MemoryBytes   uint64        `json:"memory_bytes,omitempty"`
	Uptime        time.Duration `json:"-"`
	LoadTimestamp time.Time     `json:"-"`
}

type Manager struct {
	conn *dbus.Conn
}

func NewManager() (*Manager, error) {
	conn, err := dbus.New()
	if err != nil {
		return nil, fmt.Errorf("dbus connect: %w", err)
	}
	return &Manager{conn: conn}, nil
}

func (m *Manager) Close() {
	if m.conn != nil {
		m.conn.Close()
	}
}

func (m *Manager) ListUnits() ([]Unit, error) {
	statuses, err := m.conn.ListUnits()
	if err != nil {
		return nil, fmt.Errorf("list units: %w", err)
	}

	units := make([]Unit, 0, len(statuses))
	for _, s := range statuses {
		pid := 0
		props, err := m.conn.GetUnitProperties(s.Name)
		if err == nil {
			if v, ok := props["MainPID"]; ok {
				if pidVal, ok := v.(uint32); ok {
					pid = int(pidVal)
				}
			}
		}

		units = append(units, Unit{
			Name:          s.Name,
			Description:   s.Description,
			ActiveState:   s.ActiveState,
			SubState:      s.SubState,
			LoadState:     s.LoadState,
			PID:           pid,
			LoadTimestamp: time.Now(),
		})
	}
	return units, nil
}

func (m *Manager) StartUnit(name string) (int, error) {
	jobID, err := m.conn.StartUnit(name, "replace", nil)
	if err != nil {
		return 0, fmt.Errorf("start %s: %w", name, err)
	}
	return jobID, nil
}

func (m *Manager) StopUnit(name string) (int, error) {
	jobID, err := m.conn.StopUnit(name, "replace", nil)
	if err != nil {
		return 0, fmt.Errorf("stop %s: %w", name, err)
	}
	return jobID, nil
}

func (m *Manager) RestartUnit(name string) (int, error) {
	jobID, err := m.conn.RestartUnit(name, "replace", nil)
	if err != nil {
		return 0, fmt.Errorf("restart %s: %w", name, err)
	}
	return jobID, nil
}

func (m *Manager) GetUnitProperties(name string) (map[string]interface{}, error) {
	props, err := m.conn.GetUnitProperties(name)
	if err != nil {
		return nil, fmt.Errorf("get properties %s: %w", name, err)
	}
	return props, nil
}

func (m *Manager) SubscribeUnits() (<-chan map[string]*dbus.UnitStatus, <-chan error) {
	updates := make(chan map[string]*dbus.UnitStatus)
	errChan := make(chan error, 1)

	internalUpdates, internalErrors := m.conn.SubscribeUnits(100 * time.Millisecond)
	go func() {
		for {
			select {
			case u, ok := <-internalUpdates:
				if !ok {
					close(updates)
					return
				}
				updates <- u
			case e := <-internalErrors:
				errChan <- e
			}
		}
	}()
	return updates, errChan
}
