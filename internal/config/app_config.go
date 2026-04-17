package config

import (
	"fmt"
	"strings"
	"time"
)

type AppConfiguration struct {
	Name                    string                `yaml:"name"`
	Host                    string                `yaml:"host"`
	Ports                   AppPortsConfiguration `yaml:"ports"`
	OutputPorts             AppPortsConfiguration `yaml:"output_ports"`
	SessionChallengeTTL     time.Duration         `yaml:"session_challenge_ttl"`
	EventRetention          time.Duration         `yaml:"event_retention"`
	EventBatchSize          int                   `yaml:"event_batch_size"`
	EventRedeliveryCooldown time.Duration         `yaml:"event_redelivery_cooldown"`
	OutboxPollInterval      time.Duration         `yaml:"outbox_poll_interval"`
	OutboxBatchSizeSegments int                   `yaml:"outbox_batch_size_segments"`
	OutboxAckTimeout        time.Duration         `yaml:"outbox_ack_timeout"`
	OutboxMaxAttempts       int                   `yaml:"outbox_max_attempts"`
	OutboxJanitorInterval   time.Duration         `yaml:"outbox_janitor_interval"`
	OutboxAckRetention      time.Duration         `yaml:"outbox_ack_retention"`
	OutboxDropRetention     time.Duration         `yaml:"outbox_drop_retention"`
	TLS                     AppTLSConfiguration   `yaml:"tls"`
}

type AppPortsConfiguration struct {
	TCPPort    int `yaml:"tcp_port"`
	TCPTLSPort int `yaml:"tcp_tls_port"`
	HTTPPort   int `yaml:"http_port"`
	HTTPSPort  int `yaml:"https_port"`
	WSPort     int `yaml:"ws_port"`
	WSSPort    int `yaml:"wss_port"`
}

type AppTLSConfiguration struct {
	CertFile string `yaml:"cert_file"`
	KeyFile  string `yaml:"key_file"`
}

func (a AppConfiguration) Validate() error {
	if strings.TrimSpace(a.Name) == "" {
		return fmt.Errorf("app.name is required")
	}

	if strings.TrimSpace(a.Host) == "" {
		return fmt.Errorf("app.host is required")
	}

	if err := a.Ports.ValidateInternal("app.ports"); err != nil {
		return err
	}

	if err := a.OutputPorts.ValidatePublic("app.output_ports"); err != nil {
		return err
	}
	if a.SessionChallengeTTL <= 0 {
		return fmt.Errorf("app.session_challenge_ttl must be > 0")
	}
	if a.EventRetention <= 0 {
		return fmt.Errorf("app.event_retention must be > 0")
	}
	if a.EventBatchSize <= 0 {
		return fmt.Errorf("app.event_batch_size must be > 0")
	}
	if a.EventRedeliveryCooldown < 0 {
		return fmt.Errorf("app.event_redelivery_cooldown must be >= 0")
	}
	if a.OutboxPollInterval < 0 {
		return fmt.Errorf("app.outbox_poll_interval must be >= 0")
	}
	if a.OutboxBatchSizeSegments < 0 {
		return fmt.Errorf("app.outbox_batch_size_segments must be >= 0")
	}
	if a.OutboxAckTimeout < 0 {
		return fmt.Errorf("app.outbox_ack_timeout must be >= 0")
	}
	if a.OutboxMaxAttempts < 0 {
		return fmt.Errorf("app.outbox_max_attempts must be >= 0")
	}
	if a.OutboxJanitorInterval < 0 {
		return fmt.Errorf("app.outbox_janitor_interval must be >= 0")
	}
	if a.OutboxAckRetention < 0 {
		return fmt.Errorf("app.outbox_ack_retention must be >= 0")
	}
	if a.OutboxDropRetention < 0 {
		return fmt.Errorf("app.outbox_drop_retention must be >= 0")
	}

	return nil
}

func (p AppPortsConfiguration) ValidateInternal(section string) error {
	if strings.TrimSpace(section) == "" {
		section = "app.ports"
	}

	if p.TCPPort <= 0 {
		return fmt.Errorf("%s.tcp_port must be > 0", section)
	}

	if p.HTTPPort <= 0 {
		return fmt.Errorf("%s.http_port must be > 0", section)
	}

	if p.WSPort <= 0 {
		return fmt.Errorf("%s.ws_port must be > 0", section)
	}

	duplicates := map[int][]string{}
	register := func(port int, name string) {
		duplicates[port] = append(duplicates[port], name)
	}

	register(p.TCPPort, "tcp_port")
	register(p.HTTPPort, "http_port")
	register(p.WSPort, "ws_port")

	for port, names := range duplicates {
		if len(names) < 2 {
			continue
		}

		if isAllowedDuplicate(names, "http_port", "ws_port") {
			continue
		}

		return fmt.Errorf("port %d is reused by unsupported combination: %s", port, strings.Join(names, ", "))
	}

	return nil
}

func (p AppPortsConfiguration) ValidatePublic(section string) error {
	if strings.TrimSpace(section) == "" {
		section = "app.output_ports"
	}

	if p.TCPPort <= 0 {
		return fmt.Errorf("%s.tcp_port must be > 0", section)
	}
	if p.TCPTLSPort <= 0 {
		return fmt.Errorf("%s.tcp_tls_port must be > 0", section)
	}
	if p.HTTPPort <= 0 {
		return fmt.Errorf("%s.http_port must be > 0", section)
	}
	if p.HTTPSPort <= 0 {
		return fmt.Errorf("%s.https_port must be > 0", section)
	}
	if p.WSPort <= 0 {
		return fmt.Errorf("%s.ws_port must be > 0", section)
	}
	if p.WSSPort <= 0 {
		return fmt.Errorf("%s.wss_port must be > 0", section)
	}

	return nil
}

func isAllowedDuplicate(names []string, first string, second string) bool {
	if len(names) != 2 {
		return false
	}

	if names[0] == first && names[1] == second {
		return true
	}

	if names[0] == second && names[1] == first {
		return true
	}

	return false
}
