package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"gopkg.in/yaml.v3"
)

type Config struct {
	AppName string `yaml:"app_name"`
	Debug   bool   `yaml:"debug"`

	Support SupportConfig `yaml:"support"`
	Broker  BrokerConfig  `yaml:"broker"`

	BaseDir []string `yaml:"basedir"`
	Pool    []string `yaml:"pool"`
	Cells   []string `yaml:"cells"`

	AwaitTimeoutSeconds       int `yaml:"await-timeout_seconds"`
	AwaitSupportRebootSeconds int `yaml:"await_support_reboot_seconds"`
}

type SupportConfig struct {
	Port  string `yaml:"port"`
	Debug bool   `yaml:"debug"`
}

type BrokerConfig struct {
	Port     string `yaml:"port"`
	Protocol string `yaml:"protocol"`
	Codec    string `yaml:"codec"`
	Debug    bool   `yaml:"debug"`
}

type PoolConfig struct {
	AgentTypes []AgentTypeConfig `yaml:"agent_types"`
}

type AgentTypeConfig struct {
	AgentType    string   `yaml:"agent_type"`
	Binary       string   `yaml:"binary"`
	Operator     string   `yaml:"operator"`
	Capabilities []string `yaml:"capabilities"`
	Description  string   `yaml:"description"`
}

type CellsConfig struct {
	Cells []Cell `yaml:"cells,omitempty"`
}

type Cell struct {
	ID          string      `yaml:"id"`
	Description string      `yaml:"description"`
	Debug       bool        `yaml:"debug"`
	Agents      []CellAgent `yaml:"agents"`
}

type CellAgent struct {
	ID        string                 `yaml:"id"`
	AgentType string                 `yaml:"agent_type"`
	Ingress   string                 `yaml:"ingress"`
	Egress    string                 `yaml:"egress"`
	Config    map[string]interface{} `yaml:"config,omitempty"`
}

func Load(filename string) (*Config, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	// Set defaults
	if config.Support.Port == "" {
		config.Support.Port = ":9000"
	}
	if config.Broker.Port == "" {
		config.Broker.Port = ":9001"
	}
	if config.Broker.Protocol == "" {
		config.Broker.Protocol = "tcp"
	}
	if config.Broker.Codec == "" {
		config.Broker.Codec = "json"
	}
	if config.AwaitTimeoutSeconds == 0 {
		config.AwaitTimeoutSeconds = 300
	}
	if config.AwaitSupportRebootSeconds == 0 {
		config.AwaitSupportRebootSeconds = 300
	}

	// Validate configuration values
	if config.AwaitTimeoutSeconds < 0 {
		return nil, fmt.Errorf("await timeout seconds cannot be negative: %d", config.AwaitTimeoutSeconds)
	}
	if config.AwaitSupportRebootSeconds < 0 {
		return nil, fmt.Errorf("await support reboot seconds cannot be negative: %d", config.AwaitSupportRebootSeconds)
	}

	return &config, nil
}

func (c *Config) LoadPool() (*PoolConfig, error) {
	if len(c.Pool) == 0 {
		return &PoolConfig{}, nil
	}

	poolFile := c.Pool[0]
	if !filepath.IsAbs(poolFile) {
		if len(c.BaseDir) > 0 {
			poolFile = filepath.Join(c.BaseDir[0], poolFile)
		}
	}

	data, err := os.ReadFile(poolFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read pool file %s: %w", poolFile, err)
	}

	var poolConfig struct {
		Pool PoolConfig `yaml:"pool"`
	}
	if err := yaml.Unmarshal(data, &poolConfig); err != nil {
		return nil, fmt.Errorf("failed to parse pool file %s: %w", poolFile, err)
	}

	return &poolConfig.Pool, nil
}

func (c *Config) LoadCells() (*CellsConfig, error) {
	if len(c.Cells) == 0 {
		return &CellsConfig{}, nil
	}

	cellsFile := c.Cells[0]
	if !filepath.IsAbs(cellsFile) {
		if len(c.BaseDir) > 0 {
			cellsFile = filepath.Join(c.BaseDir[0], cellsFile)
		}
	}

	data, err := os.ReadFile(cellsFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read cells file %s: %w", cellsFile, err)
	}

	// Handle multiple YAML documents separated by ---
	var cells []Cell

	decoder := yaml.NewDecoder(bytes.NewReader(data))
	for {
		var cellDoc struct {
			Cell Cell `yaml:"cell"`
		}
		if err := decoder.Decode(&cellDoc); err != nil {
			if err.Error() == "EOF" {
				break
			}
			return nil, fmt.Errorf("failed to parse cells file %s: %w", cellsFile, err)
		}
		if cellDoc.Cell.ID != "" {
			cells = append(cells, cellDoc.Cell)
		}
	}

	return &CellsConfig{Cells: cells}, nil
}

// Helper function to convert timeout strings to integers
func ParseTimeout(timeoutStr string) (int, error) {
	if timeoutStr == "" {
		return 0, nil
	}
	return strconv.Atoi(timeoutStr)
}
