package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tenzoki/gox/internal/client"
)

// IngressHandler abstracts ingress connection types
type IngressHandler interface {
	Connect(config string, base *BaseAgent) (<-chan *client.BrokerMessage, error)
	Type() string
}

// EgressHandler abstracts egress connection types  
type EgressHandler interface {
	Send(config string, msg *client.BrokerMessage, base *BaseAgent) error
	Type() string
}

// ConnectionHandlers manages ingress and egress handlers
type ConnectionHandlers struct {
	ingress IngressHandler
	egress  EgressHandler
}

// NewConnectionHandlers creates handlers based on connection configurations
func NewConnectionHandlers(ingressConfig, egressConfig string, base *BaseAgent) (*ConnectionHandlers, error) {
	ingress, err := NewIngressHandler(ingressConfig, base)
	if err != nil {
		return nil, fmt.Errorf("failed to create ingress handler: %w", err)
	}

	egress, err := NewEgressHandler(egressConfig, base)
	if err != nil {
		return nil, fmt.Errorf("failed to create egress handler: %w", err)
	}

	return &ConnectionHandlers{
		ingress: ingress,
		egress:  egress,
	}, nil
}

// Connect establishes ingress connection
func (h *ConnectionHandlers) Connect() (<-chan *client.BrokerMessage, error) {
	return h.ingress.Connect("", nil) // Config already parsed in constructor
}

// Send sends message via egress
func (h *ConnectionHandlers) Send(msg *client.BrokerMessage) error {
	return h.egress.Send("", msg, nil) // Config already parsed in constructor
}

// --- INGRESS HANDLERS ---

// SubscriptionIngressHandler handles "sub:" connections
type SubscriptionIngressHandler struct {
	topicName string
	base      *BaseAgent
}

func (s *SubscriptionIngressHandler) Type() string { return "subscription" }

func (s *SubscriptionIngressHandler) Connect(config string, base *BaseAgent) (<-chan *client.BrokerMessage, error) {
	msgChan, err := s.base.BrokerClient.Subscribe(s.topicName)
	if err != nil {
		return nil, fmt.Errorf("failed to subscribe to topic %s: %w", s.topicName, err)
	}
	s.base.LogInfo("Subscribed to topic: %s", s.topicName)
	return msgChan, nil
}

// PipeIngressHandler handles "pipe:" connections (consumer)
type PipeIngressHandler struct {
	pipeName string
	base     *BaseAgent
}

func (p *PipeIngressHandler) Type() string { return "pipe_consumer" }

func (p *PipeIngressHandler) Connect(config string, base *BaseAgent) (<-chan *client.BrokerMessage, error) {
	if err := p.base.BrokerClient.ConnectPipe(p.pipeName, "consumer"); err != nil {
		return nil, fmt.Errorf("failed to connect to pipe %s: %w", p.pipeName, err)
	}
	p.base.LogInfo("Connected to pipe as consumer: %s", p.pipeName)
	
	// For pipe consumers, we need to create a channel and handle receiving in a goroutine
	// This matches the current file_writer implementation
	msgChan := make(chan *client.BrokerMessage, 10)
	
	go func() {
		defer close(msgChan)
		for {
			select {
			case <-p.base.Context().Done():
				p.base.LogDebug("Pipe ingress handler shutting down")
				return
			default:
				msg, err := p.base.BrokerClient.ReceivePipe(p.pipeName, 1000) // 1 second timeout
				if err != nil {
					if !strings.Contains(err.Error(), "Timeout waiting for message") {
						p.base.LogError("Failed to receive from pipe: %v", err)
					}
					time.Sleep(100 * time.Millisecond)
					continue
				}
				
				if msg != nil {
					if brokerMsg, ok := msg.(*client.BrokerMessage); ok {
						// Debug: Log message received from pipe
						p.base.LogDebug("PipeIngressHandler received message %s from pipe %s with meta: %+v", brokerMsg.ID, p.pipeName, brokerMsg.Meta)
						for k, v := range brokerMsg.Meta {
							p.base.LogDebug("  Received Meta[%s] = %+v (type: %T)", k, v, v)
						}
						
						select {
						case msgChan <- brokerMsg:
						case <-p.base.Context().Done():
							return
						}
					}
				}
			}
		}
	}()
	
	return msgChan, nil
}

// FileIngressConfig represents configuration for file ingester
type FileIngressConfig struct {
	Digest         bool   `json:"digest"`          // Enable digest-based duplicate prevention (default: true)
	DigestStrategy string `json:"digest_strategy"` // Strategy after successful processing: "delete", "move", "none" (default: "delete")
	DigestFile     string `json:"digest_file"`     // File to store processed file digests (default: ".gox_digests")
	MoveDir        string `json:"move_dir"`        // Directory to move files when digest_strategy is "move"
}

// ProcessedFile represents a file that has been processed
type ProcessedFile struct {
	Path   string    `json:"path"`
	Digest string    `json:"digest"`
	Time   time.Time `json:"time"`
}

// FileIngressHandler handles "file:" connections (directory watching)
type FileIngressHandler struct {
	watchDir          string
	filePattern       string  // File pattern to match (e.g., "*.txt")
	base              *BaseAgent
	config            FileIngressConfig
	processedDigests  map[string]ProcessedFile
	digestFilePath    string
}

func (f *FileIngressHandler) Type() string { return "file_watcher" }

func (f *FileIngressHandler) Connect(config string, base *BaseAgent) (<-chan *client.BrokerMessage, error) {
	// Initialize configuration with defaults
	f.config = FileIngressConfig{
		Digest:         true,     // Default: enable digest
		DigestStrategy: "delete", // Default: delete files after successful processing
		DigestFile:     ".gox_digests",
		MoveDir:        "",
	}

	// Parse processor configuration from base agent (synced from support)
	if digest := f.base.GetConfigBool("digest", true); !digest {
		f.config.Digest = false
	}
	f.config.DigestStrategy = f.base.GetConfigString("digest_strategy", "delete")
	f.config.DigestFile = f.base.GetConfigString("digest_file", ".gox_digests")
	f.config.MoveDir = f.base.GetConfigString("move_dir", "")

	// Get absolute path for better logging
	absWatchDir, err := filepath.Abs(f.watchDir)
	if err != nil {
		f.base.LogError("Failed to get absolute path for %s: %v", f.watchDir, err)
		absWatchDir = f.watchDir
	}

	// Load processed files digest cache if digest is enabled
	f.processedDigests = make(map[string]ProcessedFile)
	if f.config.Digest {
		f.digestFilePath = filepath.Join(f.watchDir, f.config.DigestFile)
		if absDigestPath, err := filepath.Abs(f.digestFilePath); err == nil {
			f.digestFilePath = absDigestPath
		}
		f.processedDigests = f.loadDigestCache()
		f.base.LogInfo("Digest mode enabled, using %s (loaded %d entries)", f.digestFilePath, len(f.processedDigests))
	}

	f.base.LogInfo("File ingester watching: %s (pattern: %s)", absWatchDir, f.filePattern)
	f.base.LogDebug("Config: digest=%v, strategy=%s, digestFile=%s",
		f.config.Digest, f.config.DigestStrategy, f.config.DigestFile)

	// Create message channel
	msgChan := make(chan *client.BrokerMessage, 10)
	
	// Start file watching goroutine
	go func() {
		defer close(msgChan)
		seen := make(map[string]struct{})
		
		for {
			select {
			case <-f.base.Context().Done():
				f.base.LogInfo("File watcher shutting down")
				return
			default:
			}

			files, err := os.ReadDir(f.watchDir)
			if err != nil {
				if os.IsNotExist(err) {
					// Try to create the directory
					if createErr := os.MkdirAll(f.watchDir, 0755); createErr != nil {
						f.base.LogError("Failed to create watch directory %s: %v", absWatchDir, createErr)
					} else {
						f.base.LogInfo("Created watch directory: %s", absWatchDir)
						// Continue to next iteration to start watching the new directory
						select {
						case <-f.base.Context().Done():
							return
						case <-time.After(2 * time.Second):
							continue
						}
					}
				} else {
					f.base.LogError("Failed to read directory %s: %v", absWatchDir, err)
				}
				select {
				case <-f.base.Context().Done():
					return
				case <-time.After(5 * time.Second):
					continue
				}
			}

			for _, fileInfo := range files {
				if fileInfo.IsDir() {
					continue
				}
				
				// Skip if file doesn't match the pattern
				if f.filePattern != "*" && f.filePattern != "" {
					matched, err := filepath.Match(f.filePattern, fileInfo.Name())
					if err != nil {
						f.base.LogError("Invalid file pattern %s: %v", f.filePattern, err)
						continue
					}
					if !matched {
						continue
					}
				}
				
				path := filepath.Join(f.watchDir, fileInfo.Name())
				if _, ok := seen[path]; ok {
					continue
				}
				seen[path] = struct{}{}

				// Get absolute path for this file
				absPath, err := filepath.Abs(path)
				if err != nil {
					absPath = path
				}

				// Check digest if enabled
				var fileDigest string
				if f.config.Digest {
					fileDigest, err = f.calculateFileDigest(path)
					if err != nil {
						f.base.LogError("Failed to calculate digest for %s: %v", absPath, err)
						continue
					}

					if _, alreadyProcessed := f.processedDigests[fileDigest]; alreadyProcessed {
						f.base.LogDebug("Skipping already processed file: %s (digest: %s)", absPath, fileDigest)
						continue
					}

					f.base.LogDebug("New file detected: %s (digest: %s)", absPath, fileDigest)
				}

				// Read file content
				data, err := os.ReadFile(path)
				if err != nil {
					f.base.LogError("Failed to read file %s: %v", absPath, err)
					continue
				}

				// Create message for broker
				msg := &client.BrokerMessage{
					ID:      fmt.Sprintf("file_%d", time.Now().UnixNano()),
					Type:    "file",
					Payload: string(data), // Convert to string for JSON transport
					Meta: map[string]interface{}{
						"filename":    fileInfo.Name(),
						"filepath":    absPath,
						"size":        len(data),
						"ingested_at": time.Now(),
					},
				}
				
				// Debug: Log the metadata being set
				f.base.LogDebug("FileIngressHandler creating message with filename: %s", fileInfo.Name())
				for k, v := range msg.Meta {
					f.base.LogDebug("  Setting Meta[%s] = %+v (type: %T)", k, v, v)
				}

				select {
				case msgChan <- msg:
					f.base.LogInfo("Published file: %s (%d bytes)", absPath, len(data))
					
					// Process file according to digest strategy after successful ingestion
					if f.config.Digest {
						processedFile := ProcessedFile{
							Path:   absPath,
							Digest: fileDigest,
							Time:   time.Now(),
						}
						f.processedDigests[fileDigest] = processedFile

						// Save digest cache
						if err := f.saveDigestCache(); err != nil {
							f.base.LogError("Failed to save digest cache: %v", err)
						}

						// Apply digest strategy
						if err := f.applyDigestStrategy(path); err != nil {
							f.base.LogError("Failed to apply digest strategy %s to %s: %v",
								f.config.DigestStrategy, absPath, err)
						} else {
							f.base.LogDebug("Applied strategy '%s' to %s", f.config.DigestStrategy, absPath)
						}
					}
					
				case <-f.base.Context().Done():
					return
				}
			}

			select {
			case <-f.base.Context().Done():
				return
			case <-time.After(2 * time.Second):
				// Continue to next iteration
			}
		}
	}()

	return msgChan, nil
}

// --- EGRESS HANDLERS ---

// PublishEgressHandler handles "pub:" connections
type PublishEgressHandler struct {
	topicName string
	base      *BaseAgent
}

func (p *PublishEgressHandler) Type() string { return "publish" }

func (p *PublishEgressHandler) Send(config string, msg *client.BrokerMessage, base *BaseAgent) error {
	// Debug: Log what we're publishing
	p.base.LogDebug("PublishEgressHandler sending message %s to topic %s with meta: %+v", msg.ID, p.topicName, msg.Meta)
	for k, v := range msg.Meta {
		p.base.LogDebug("  Publishing Meta[%s] = %+v (type: %T)", k, v, v)
	}
	
	if err := p.base.BrokerClient.Publish(p.topicName, *msg); err != nil {
		return fmt.Errorf("failed to publish to topic %s: %w", p.topicName, err)
	}
	p.base.LogDebug("Published message %s to topic: %s", msg.ID, p.topicName)
	return nil
}

// PipeEgressHandler handles "pipe:" connections (producer)
type PipeEgressHandler struct {
	pipeName string
	base     *BaseAgent
}

func (p *PipeEgressHandler) Type() string { return "pipe_producer" }

func (p *PipeEgressHandler) Send(config string, msg *client.BrokerMessage, base *BaseAgent) error {
	// Debug: Log message before sending to pipe
	p.base.LogDebug("PipeEgressHandler sending message %s to pipe %s with meta: %+v", msg.ID, p.pipeName, msg.Meta)
	for k, v := range msg.Meta {
		p.base.LogDebug("  Pipe Meta[%s] = %+v (type: %T)", k, v, v)
	}
	
	if err := p.base.BrokerClient.SendPipe(p.pipeName, *msg); err != nil {
		return fmt.Errorf("failed to send to pipe %s: %w", p.pipeName, err)
	}
	p.base.LogDebug("Sent message %s to pipe: %s", msg.ID, p.pipeName)
	return nil
}

// FileEgressHandler handles "file:" connections (directory output)
type FileEgressHandler struct {
	outDir string
	base   *BaseAgent
}

func (f *FileEgressHandler) Type() string { return "file_writer" }

func (f *FileEgressHandler) Send(config string, msg *client.BrokerMessage, base *BaseAgent) error {
	// Debug: Log the entire message to see what we're receiving
	f.base.LogDebug("FileEgressHandler received message %s with meta: %+v", msg.ID, msg.Meta)
	
	// Extract filename from meta with robust type handling
	filename := "unknown_file"
	if fn, exists := msg.Meta["filename"]; exists {
		// Try multiple type assertions to handle JSON deserialization
		switch v := fn.(type) {
		case string:
			filename = v
			f.base.LogDebug("Successfully extracted filename as string: %s", filename)
		case []byte:
			filename = string(v)
			f.base.LogDebug("Successfully extracted filename from bytes: %s", filename)
		default:
			// Try converting to string via fmt.Sprintf as last resort
			filename = fmt.Sprintf("%v", fn)
			f.base.LogDebug("Filename not string, converted %T to: %s", fn, filename)
		}
	} else {
		// Debug: log all available meta keys to understand what's in the message  
		f.base.LogDebug("No filename in message meta. Available keys: %+v", msg.Meta)
		// Log each key-value pair individually for better debugging
		for k, v := range msg.Meta {
			f.base.LogDebug("Meta[%s] = %+v (type: %T)", k, v, v)
		}
	}

	// Generate output filename with timestamp
	timestamp := time.Now().UnixNano()
	outPath := filepath.Join(f.outDir, fmt.Sprintf("processed_%d_%s", timestamp, filename))

	// Get absolute path for logging
	absOutPath, err := filepath.Abs(outPath)
	if err != nil {
		absOutPath = outPath
	}

	// Convert payload to bytes
	var data []byte
	switch payload := msg.Payload.(type) {
	case string:
		data = []byte(payload)
	case []byte:
		data = payload
	default:
		return fmt.Errorf("unsupported payload type: %T", payload)
	}

	// Check if we should create directories for this file
	createDirs := f.base.GetConfigBool("create_directories", false)
	if createDirs {
		// Create directory path if it doesn't exist
		fileDir := filepath.Dir(outPath)
		if err := os.MkdirAll(fileDir, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", fileDir, err)
		}
		f.base.LogDebug("Created directory path: %s", fileDir)
	}

	// Write file
	if err := os.WriteFile(outPath, data, 0644); err != nil {
		return fmt.Errorf("write error for %s: %w", absOutPath, err)
	}

	f.base.LogInfo("Wrote file: %s (%d bytes)", absOutPath, len(data))
	f.base.LogDebug("Wrote %d bytes to %s from message %s", len(data), absOutPath, msg.ID)

	return nil
}

// --- FACTORY FUNCTIONS ---

// NewIngressHandler creates appropriate ingress handler based on config
func NewIngressHandler(config string, base *BaseAgent) (IngressHandler, error) {
	if strings.HasPrefix(config, "sub:") {
		topicName := strings.TrimPrefix(config, "sub:")
		return &SubscriptionIngressHandler{
			topicName: topicName,
			base:      base,
		}, nil
	}
	
	if strings.HasPrefix(config, "pipe:") {
		pipeName := strings.TrimPrefix(config, "pipe:")
		return &PipeIngressHandler{
			pipeName: pipeName,
			base:     base,
		}, nil
	}
	
	if strings.HasPrefix(config, "file:") {
		filePath := strings.TrimPrefix(config, "file:")
		watchDir := filepath.Dir(filePath)
		filePattern := filepath.Base(filePath) // Extract pattern from path (e.g., "*.txt")
		return &FileIngressHandler{
			watchDir:    watchDir,
			filePattern: filePattern,
			base:        base,
		}, nil
	}
	
	return nil, fmt.Errorf("unsupported ingress type: %s", config)
}

// NewEgressHandler creates appropriate egress handler based on config
func NewEgressHandler(config string, base *BaseAgent) (EgressHandler, error) {
	if strings.HasPrefix(config, "pub:") {
		topicName := strings.TrimPrefix(config, "pub:")
		return &PublishEgressHandler{
			topicName: topicName,
			base:      base,
		}, nil
	}
	
	if strings.HasPrefix(config, "pipe:") {
		pipeName := strings.TrimPrefix(config, "pipe:")
		// For egress, we need to connect as producer
		if err := base.BrokerClient.ConnectPipe(pipeName, "producer"); err != nil {
			return nil, fmt.Errorf("failed to connect to pipe %s as producer: %w", pipeName, err)
		}
		base.LogInfo("Connected to pipe as producer: %s", pipeName)
		
		return &PipeEgressHandler{
			pipeName: pipeName,
			base:     base,
		}, nil
	}
	
	if strings.HasPrefix(config, "file:") {
		filePath := strings.TrimPrefix(config, "file:")
		outDir := filepath.Dir(filePath)
		
		// Get absolute path for better logging
		absOutDir, err := filepath.Abs(outDir)
		if err != nil {
			base.LogError("Failed to get absolute path for %s: %v", outDir, err)
			absOutDir = outDir
		}

		// Ensure output directory exists
		if err := os.MkdirAll(outDir, 0755); err != nil {
			return nil, fmt.Errorf("failed to create output directory %s: %w", absOutDir, err)
		}
		
		base.LogInfo("Will write to directory: %s", absOutDir)
		
		return &FileEgressHandler{
			outDir: outDir,
			base:   base,
		}, nil
	}
	
	return nil, fmt.Errorf("unsupported egress type: %s", config)
}

// --- DIGEST HELPER METHODS FOR FileIngressHandler ---

// calculateFileDigest calculates SHA256 digest of a file
func (f *FileIngressHandler) calculateFileDigest(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}

	return hex.EncodeToString(hash.Sum(nil)), nil
}

// loadDigestCache loads the processed files cache from disk
func (f *FileIngressHandler) loadDigestCache() map[string]ProcessedFile {
	cache := make(map[string]ProcessedFile)

	if _, err := os.Stat(f.digestFilePath); os.IsNotExist(err) {
		f.base.LogDebug("Digest cache file does not exist: %s", f.digestFilePath)
		return cache
	}

	data, err := os.ReadFile(f.digestFilePath)
	if err != nil {
		f.base.LogError("Failed to read digest cache %s: %v", f.digestFilePath, err)
		return cache
	}

	var processedFiles []ProcessedFile
	if err := json.Unmarshal(data, &processedFiles); err != nil {
		f.base.LogError("Failed to parse digest cache %s: %v", f.digestFilePath, err)
		return cache
	}

	for _, pf := range processedFiles {
		cache[pf.Digest] = pf
	}

	f.base.LogDebug("Loaded %d entries from digest cache %s", len(cache), f.digestFilePath)

	return cache
}

// saveDigestCache saves the processed files cache to disk
func (f *FileIngressHandler) saveDigestCache() error {
	processedFiles := make([]ProcessedFile, 0, len(f.processedDigests))
	for _, pf := range f.processedDigests {
		processedFiles = append(processedFiles, pf)
	}

	data, err := json.MarshalIndent(processedFiles, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal digest cache: %w", err)
	}

	if err := os.WriteFile(f.digestFilePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write digest cache %s: %w", f.digestFilePath, err)
	}

	f.base.LogDebug("Saved %d entries to digest cache %s", len(f.processedDigests), f.digestFilePath)

	return nil
}

// applyDigestStrategy applies the configured strategy after successful file processing
func (f *FileIngressHandler) applyDigestStrategy(filePath string) error {
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		absPath = filePath
	}

	switch f.config.DigestStrategy {
	case "delete":
		if err := os.Remove(filePath); err != nil {
			return fmt.Errorf("failed to delete file %s: %w", absPath, err)
		}
		f.base.LogInfo("Deleted processed file: %s", absPath)

	case "move":
		if f.config.MoveDir == "" {
			return fmt.Errorf("move_dir not configured for digest_strategy 'move'")
		}

		// Ensure move directory exists
		if err := os.MkdirAll(f.config.MoveDir, 0755); err != nil {
			return fmt.Errorf("failed to create move directory %s: %w", f.config.MoveDir, err)
		}

		// Generate unique filename in move directory
		fileName := filepath.Base(filePath)
		timestamp := time.Now().Format("20060102_150405")
		newPath := filepath.Join(f.config.MoveDir, fmt.Sprintf("%s_%s", timestamp, fileName))

		if err := os.Rename(filePath, newPath); err != nil {
			return fmt.Errorf("failed to move file %s to %s: %w", absPath, newPath, err)
		}

		f.base.LogInfo("Moved processed file: %s -> %s", absPath, newPath)

	case "none":
		f.base.LogDebug("No action taken for file %s (strategy: none)", absPath)

	default:
		return fmt.Errorf("unknown digest strategy: %s", f.config.DigestStrategy)
	}

	return nil
}