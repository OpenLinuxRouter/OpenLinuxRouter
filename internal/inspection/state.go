package inspection

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// The saved switch identifies the client and addresses to resume. It lives
// beside the CA and history rather than in configuration backups: restoring a
// network configuration on another router must not arm interception there.
const settingFile = "session.json"

// setting is the switch as it outlives the process. Flows never live here.
type setting struct {
	MAC string   `json:"mac"`
	IPs []string `json:"ips"`
}

func (s *Service) readSetting() (*setting, error) {
	data, err := os.ReadFile(filepath.Join(s.Dir, settingFile))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var st setting
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, fmt.Errorf("reading %s: %w", settingFile, err)
	}
	if st.MAC == "" || len(st.IPs) == 0 {
		return nil, fmt.Errorf("reading %s: no client recorded", settingFile)
	}
	return &st, nil
}

// writeSetting replaces the switch atomically, so a crash while writing cannot
// leave half a client behind for the next start to resume.
func (s *Service) writeSetting(st *setting) error {
	data, err := json.Marshal(st)
	if err != nil {
		return err
	}
	return core.WriteFileAtomic(filepath.Join(s.Dir, settingFile), data, 0600)
}

func (s *Service) clearSetting() error {
	err := os.Remove(filepath.Join(s.Dir, settingFile))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
