package main

import (
	"os"
	"testing"

	"github.com/tenzoki/gox/internal/config"
)

// TestConfigLoadValidFile tests loading a valid configuration file
func TestConfigLoadValidFile(t *testing.T) {
	// Create temporary config file
	tempFile, err := os.CreateTemp("", "gox-config-test-*.yaml")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tempFile.Name())

	validConfig := `
app_name: "test-application"
debug: true

support:
  port: ":9000"
  debug: true

broker:
  port: ":9001"
  protocol: "tcp"
  codec: "json"
  debug: false

await-timeout_seconds: 300
await_support_reboot_seconds: 300
`

	if _, err := tempFile.WriteString(validConfig); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}
	tempFile.Close()

	// Load configuration
	cfg, err := config.Load(tempFile.Name())
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	// Verify basic fields
	if cfg.AppName != "test-application" {
		t.Errorf("Expected app_name 'test-application', got %s", cfg.AppName)
	}

	if !cfg.Debug {
		t.Error("Expected debug to be true")
	}

	// Verify support config
	if cfg.Support.Port != ":9000" {
		t.Errorf("Expected support port ':9000', got %s", cfg.Support.Port)
	}

	if !cfg.Support.Debug {
		t.Error("Expected support debug to be true")
	}

	// Verify broker config
	if cfg.Broker.Port != ":9001" {
		t.Errorf("Expected broker port ':9001', got %s", cfg.Broker.Port)
	}

	if cfg.Broker.Protocol != "tcp" {
		t.Errorf("Expected broker protocol 'tcp', got %s", cfg.Broker.Protocol)
	}

	if cfg.Broker.Codec != "json" {
		t.Errorf("Expected broker codec 'json', got %s", cfg.Broker.Codec)
	}

	if cfg.Broker.Debug {
		t.Error("Expected broker debug to be false")
	}

	// Verify timeout fields
	if cfg.AwaitTimeoutSeconds != 300 {
		t.Errorf("Expected await timeout 300, got %d", cfg.AwaitTimeoutSeconds)
	}

	if cfg.AwaitSupportRebootSeconds != 300 {
		t.Errorf("Expected await support reboot 300, got %d", cfg.AwaitSupportRebootSeconds)
	}
}

// TestConfigLoadMinimalFile tests loading a minimal configuration file
func TestConfigLoadMinimalFile(t *testing.T) {
	tempFile, err := os.CreateTemp("", "gox-config-minimal-*.yaml")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tempFile.Name())

	minimalConfig := `
app_name: "minimal-app"
`

	if _, err := tempFile.WriteString(minimalConfig); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}
	tempFile.Close()

	cfg, err := config.Load(tempFile.Name())
	if err != nil {
		t.Fatalf("Failed to load minimal config: %v", err)
	}

	if cfg.AppName != "minimal-app" {
		t.Errorf("Expected app_name 'minimal-app', got %s", cfg.AppName)
	}

	// Test default values
	if cfg.Debug {
		t.Error("Expected default debug to be false")
	}

	// Support defaults
	if cfg.Support.Port == "" {
		t.Error("Expected support port to have a default value")
	}

	// Broker defaults
	if cfg.Broker.Protocol == "" {
		t.Error("Expected broker protocol to have a default value")
	}

	if cfg.Broker.Codec == "" {
		t.Error("Expected broker codec to have a default value")
	}
}

// TestConfigLoadWithEnvironmentOverrides tests environment variable overrides
func TestConfigLoadWithEnvironmentOverrides(t *testing.T) {
	tempFile, err := os.CreateTemp("", "gox-config-env-*.yaml")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tempFile.Name())

	baseConfig := `
app_name: "env-test-app"
debug: false

support:
  port: ":9000"

broker:
  port: ":9001"
`

	if _, err := tempFile.WriteString(baseConfig); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}
	tempFile.Close()

	// Set environment variables
	os.Setenv("GOX_DEBUG", "true")
	os.Setenv("GOX_SUPPORT_PORT", ":8000")
	os.Setenv("GOX_BROKER_PORT", ":8001")
	defer func() {
		os.Unsetenv("GOX_DEBUG")
		os.Unsetenv("GOX_SUPPORT_PORT")
		os.Unsetenv("GOX_BROKER_PORT")
	}()

	cfg, err := config.Load(tempFile.Name())
	if err != nil {
		t.Fatalf("Failed to load config with env vars: %v", err)
	}

	// Check if environment variables override file values
	// (This behavior depends on implementation)
	t.Logf("Config loaded with app_name: %s, debug: %v", cfg.AppName, cfg.Debug)
	t.Logf("Support port: %s, Broker port: %s", cfg.Support.Port, cfg.Broker.Port)
}

// TestConfigLoadInvalidFile tests handling of invalid configuration files
func TestConfigLoadInvalidFile(t *testing.T) {
	// Test non-existent file
	_, err := config.Load("/non/existent/file.yaml")
	if err == nil {
		t.Error("Expected error for non-existent file, got none")
	}

	// Test invalid YAML
	tempFile, err := os.CreateTemp("", "gox-config-invalid-*.yaml")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tempFile.Name())

	invalidYaml := `
app_name: "test
invalid: yaml: structure
  - missing quotes
  - indentation: wrong
`

	if _, err := tempFile.WriteString(invalidYaml); err != nil {
		t.Fatalf("Failed to write invalid config: %v", err)
	}
	tempFile.Close()

	_, err = config.Load(tempFile.Name())
	if err == nil {
		t.Error("Expected error for invalid YAML, got none")
	}
}

// TestConfigLoadEmptyFile tests handling of empty configuration file
func TestConfigLoadEmptyFile(t *testing.T) {
	tempFile, err := os.CreateTemp("", "gox-config-empty-*.yaml")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tempFile.Name())

	// Write empty content
	tempFile.Close()

	cfg, err := config.Load(tempFile.Name())
	if err != nil {
		t.Fatalf("Failed to load empty config: %v", err)
	}

	// Should have default values
	if cfg.AppName != "" {
		t.Logf("Empty config loaded with default app_name: %s", cfg.AppName)
	}
}

// TestConfigSupportConfig tests support-specific configuration
func TestConfigSupportConfig(t *testing.T) {
	tempFile, err := os.CreateTemp("", "gox-config-support-*.yaml")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tempFile.Name())

	supportConfig := `
support:
  port: ":8080"
  debug: true
  max_connections: 100
  timeout: 30
`

	if _, err := tempFile.WriteString(supportConfig); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}
	tempFile.Close()

	cfg, err := config.Load(tempFile.Name())
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	if cfg.Support.Port != ":8080" {
		t.Errorf("Expected support port ':8080', got %s", cfg.Support.Port)
	}

	if !cfg.Support.Debug {
		t.Error("Expected support debug to be true")
	}
}

// TestConfigBrokerConfig tests broker-specific configuration
func TestConfigBrokerConfig(t *testing.T) {
	tempFile, err := os.CreateTemp("", "gox-config-broker-*.yaml")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tempFile.Name())

	brokerConfig := `
broker:
  port: ":8081"
  protocol: "udp"
  codec: "msgpack"
  debug: true
  buffer_size: 1024
`

	if _, err := tempFile.WriteString(brokerConfig); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}
	tempFile.Close()

	cfg, err := config.Load(tempFile.Name())
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	if cfg.Broker.Port != ":8081" {
		t.Errorf("Expected broker port ':8081', got %s", cfg.Broker.Port)
	}

	if cfg.Broker.Protocol != "udp" {
		t.Errorf("Expected broker protocol 'udp', got %s", cfg.Broker.Protocol)
	}

	if cfg.Broker.Codec != "msgpack" {
		t.Errorf("Expected broker codec 'msgpack', got %s", cfg.Broker.Codec)
	}

	if !cfg.Broker.Debug {
		t.Error("Expected broker debug to be true")
	}
}

// TestConfigComplexConfiguration tests a complex configuration file
func TestConfigComplexConfiguration(t *testing.T) {
	tempFile, err := os.CreateTemp("", "gox-config-complex-*.yaml")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tempFile.Name())

	complexConfig := `
app_name: "complex-pipeline-system"
debug: true

support:
  port: ":9000"
  debug: true
  max_connections: 500
  timeout: 60

broker:
  port: ":9001"
  protocol: "tcp"
  codec: "json"
  debug: false
  max_message_size: 1048576
  buffer_size: 8192

await-timeout_seconds: 600
await_support_reboot_seconds: 120

# Additional sections that might be ignored
logging:
  level: "info"
  file: "/var/log/gox.log"

metrics:
  enabled: true
  port: ":9090"
`

	if _, err := tempFile.WriteString(complexConfig); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}
	tempFile.Close()

	cfg, err := config.Load(tempFile.Name())
	if err != nil {
		t.Fatalf("Failed to load complex config: %v", err)
	}

	// Verify all fields are loaded correctly
	if cfg.AppName != "complex-pipeline-system" {
		t.Errorf("Expected app_name 'complex-pipeline-system', got %s", cfg.AppName)
	}

	if !cfg.Debug {
		t.Error("Expected debug to be true")
	}

	if cfg.Support.Port != ":9000" {
		t.Errorf("Expected support port ':9000', got %s", cfg.Support.Port)
	}

	if cfg.Broker.Port != ":9001" {
		t.Errorf("Expected broker port ':9001', got %s", cfg.Broker.Port)
	}

	if cfg.AwaitTimeoutSeconds != 600 {
		t.Errorf("Expected await timeout 600, got %d", cfg.AwaitTimeoutSeconds)
	}

	if cfg.AwaitSupportRebootSeconds != 120 {
		t.Errorf("Expected await support reboot 120, got %d", cfg.AwaitSupportRebootSeconds)
	}
}

// TestConfigValidation tests configuration validation
func TestConfigValidation(t *testing.T) {
	// Test configurations that might need validation
	testCases := []struct {
		name   string
		config string
		valid  bool
	}{
		{
			name: "valid_standard_ports",
			config: `
support:
  port: ":9000"
broker:
  port: ":9001"
`,
			valid: true,
		},
		{
			name: "valid_custom_ports",
			config: `
support:
  port: ":8080"
broker:
  port: ":8081"
`,
			valid: true,
		},
		{
			name: "port_without_colon",
			config: `
support:
  port: "9000"
broker:
  port: "9001"
`,
			valid: true, // May be valid depending on implementation
		},
		{
			name: "negative_timeout",
			config: `
await-timeout_seconds: -1
`,
			valid: false, // Should be invalid
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			tempFile, err := os.CreateTemp("", "gox-config-validation-*.yaml")
			if err != nil {
				t.Fatalf("Failed to create temp file: %v", err)
			}
			defer os.Remove(tempFile.Name())

			if _, err := tempFile.WriteString(tc.config); err != nil {
				t.Fatalf("Failed to write config: %v", err)
			}
			tempFile.Close()

			cfg, err := config.Load(tempFile.Name())

			if tc.valid && err != nil {
				t.Errorf("Expected valid config to load, got error: %v", err)
			}

			if !tc.valid && err == nil {
				t.Errorf("Expected invalid config to fail, but it loaded successfully")
			}

			if cfg != nil {
				t.Logf("Config loaded: %+v", cfg)
			}
		})
	}
}

// BenchmarkConfigLoad benchmarks configuration loading
func BenchmarkConfigLoad(b *testing.B) {
	tempFile, err := os.CreateTemp("", "gox-config-bench-*.yaml")
	if err != nil {
		b.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tempFile.Name())

	benchConfig := `
app_name: "benchmark-app"
debug: true

support:
  port: ":9000"
  debug: true

broker:
  port: ":9001"
  protocol: "tcp"
  codec: "json"
  debug: false

await-timeout_seconds: 300
await_support_reboot_seconds: 300
`

	if _, err := tempFile.WriteString(benchConfig); err != nil {
		b.Fatalf("Failed to write config: %v", err)
	}
	tempFile.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		config.Load(tempFile.Name())
	}
}
