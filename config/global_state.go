package config

import (
	"claude-squad/log"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const (
	GlobalStateFileName = "global_state.json"
	ProjectsDirName     = "projects"
)

// GlobalProjectData represents a project's metadata in global state
type GlobalProjectData struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	RepoPath    string    `json:"repo_path"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	InstanceCount int     `json:"instance_count"`
}

// GlobalState represents the global application state
type GlobalState struct {
	Projects          []GlobalProjectData `json:"projects"`
	HelpScreensSeen   uint32              `json:"help_screens_seen"`
	LastMigrationVersion int               `json:"last_migration_version"`
}

// GlobalStateManager handles global state operations
type GlobalStateManager struct {
	configDir string
	state     *GlobalState
}

// NewGlobalStateManager creates a new global state manager
func NewGlobalStateManager(configDir string) *GlobalStateManager {
	return &GlobalStateManager{
		configDir: configDir,
	}
}

// LoadGlobalState loads the global state from disk
func (gsm *GlobalStateManager) LoadGlobalState() (*GlobalState, error) {
	log.InfoLog.Printf("[GLOBAL-STATE] Loading global state from disk")

	statePath := filepath.Join(gsm.configDir, GlobalStateFileName)
	log.InfoLog.Printf("[GLOBAL-STATE] Global state file path: %s", statePath)

	data, err := os.ReadFile(statePath)
	if err != nil {
		if os.IsNotExist(err) {
			log.InfoLog.Printf("[GLOBAL-STATE] Global state file not found, returning default state")
			// Return default state if file doesn't exist
			return gsm.DefaultGlobalState(), nil
		}
		return nil, fmt.Errorf("failed to read global state: %w", err)
	}

	log.InfoLog.Printf("[GLOBAL-STATE] Successfully read global state file, size: %d bytes", len(data))

	var state GlobalState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("failed to parse global state: %w", err)
	}

	log.InfoLog.Printf("[GLOBAL-STATE] Parsed global state: %d projects, help screens seen: %d",
		len(state.Projects), state.HelpScreensSeen)

	gsm.state = &state
	return &state, nil
}

// SaveGlobalState saves the global state to disk
func (gsm *GlobalStateManager) SaveGlobalState() error {
	if gsm.state == nil {
		return fmt.Errorf("no state loaded")
	}

	statePath := filepath.Join(gsm.configDir, GlobalStateFileName)
	data, err := json.MarshalIndent(gsm.state, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal global state: %w", err)
	}

	if err := os.MkdirAll(gsm.configDir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	if err := os.WriteFile(statePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write global state: %w", err)
	}

	return nil
}

// DefaultGlobalState returns the default global state
func (gsm *GlobalStateManager) DefaultGlobalState() *GlobalState {
	return &GlobalState{
		Projects:             []GlobalProjectData{},
		HelpScreensSeen:      0,
		LastMigrationVersion: 0,
	}
}

// GetOrCreateGlobalState loads or creates the global state
func (gsm *GlobalStateManager) GetOrCreateGlobalState() (*GlobalState, error) {
	if gsm.state != nil {
		return gsm.state, nil
	}

	state, err := gsm.LoadGlobalState()
	if err != nil {
		log.WarningLog.Printf("Failed to load global state, creating default: %v", err)
		state = gsm.DefaultGlobalState()
	}

	gsm.state = state
	return state, nil
}

// GetProject retrieves a project by ID
func (gsm *GlobalStateManager) GetProject(projectID string) (*GlobalProjectData, error) {
	log.InfoLog.Printf("[GLOBAL-STATE] GetProject called for ID: %s", projectID)

	state, err := gsm.GetOrCreateGlobalState()
	if err != nil {
		return nil, err
	}

	log.InfoLog.Printf("[GLOBAL-STATE] Searching through %d projects", len(state.Projects))
	for i := range state.Projects {
		log.InfoLog.Printf("[GLOBAL-STATE] Comparing with project ID: %s", state.Projects[i].ID)
		if state.Projects[i].ID == projectID {
			log.InfoLog.Printf("[GLOBAL-STATE] Found project: %s", state.Projects[i].Name)
			return &state.Projects[i], nil
		}
	}

	log.InfoLog.Printf("[GLOBAL-STATE] Project not found: %s", projectID)
	return nil, nil // Project not found
}

// AddProject adds a new project to global state
func (gsm *GlobalStateManager) AddProject(projectID, name, repoPath string) error {
	log.InfoLog.Printf("[GLOBAL-STATE] AddProject called: ID=%s, Name=%s, Path=%s", projectID, name, repoPath)

	state, err := gsm.GetOrCreateGlobalState()
	if err != nil {
		return err
	}

	// Check if project already exists
	for i := range state.Projects {
		if state.Projects[i].ID == projectID {
			log.InfoLog.Printf("[GLOBAL-STATE] Project already exists, updating: %s", projectID)
			// Update existing project
			state.Projects[i].Name = name
			state.Projects[i].RepoPath = repoPath
			state.Projects[i].UpdatedAt = time.Now()
			return gsm.SaveGlobalState()
		}
	}

	log.InfoLog.Printf("[GLOBAL-STATE] Creating new project: %s", projectID)
	// Add new project
	newProject := GlobalProjectData{
		ID:          projectID,
		Name:        name,
		RepoPath:    repoPath,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
		InstanceCount: 0,
	}

	state.Projects = append(state.Projects, newProject)
	log.InfoLog.Printf("[GLOBAL-STATE] Added new project, total projects: %d", len(state.Projects))
	return gsm.SaveGlobalState()
}

// UpdateProjectInstanceCount updates the instance count for a project
func (gsm *GlobalStateManager) UpdateProjectInstanceCount(projectID string, count int) error {
	state, err := gsm.GetOrCreateGlobalState()
	if err != nil {
		return err
	}

	for i := range state.Projects {
		if state.Projects[i].ID == projectID {
			state.Projects[i].InstanceCount = count
			state.Projects[i].UpdatedAt = time.Now()
			return gsm.SaveGlobalState()
		}
	}

	return fmt.Errorf("project not found: %s", projectID)
}

// GetAllProjects returns all projects in global state
func (gsm *GlobalStateManager) GetAllProjects() ([]GlobalProjectData, error) {
	state, err := gsm.GetOrCreateGlobalState()
	if err != nil {
		return nil, err
	}

	return state.Projects, nil
}

// RemoveProject removes a project from global state
func (gsm *GlobalStateManager) RemoveProject(projectID string) error {
	state, err := gsm.GetOrCreateGlobalState()
	if err != nil {
		return err
	}

	newProjects := make([]GlobalProjectData, 0)
	found := false
	for _, project := range state.Projects {
		if project.ID != projectID {
			newProjects = append(newProjects, project)
		} else {
			found = true
		}
	}

	if !found {
		return fmt.Errorf("project not found: %s", projectID)
	}

	state.Projects = newProjects
	return gsm.SaveGlobalState()
}

// GetHelpScreensSeen returns the bitmask of seen help screens
func (gsm *GlobalStateManager) GetHelpScreensSeen() uint32 {
	state, err := gsm.GetOrCreateGlobalState()
	if err != nil {
		return 0
	}
	return state.HelpScreensSeen
}

// SetHelpScreensSeen updates the bitmask of seen help screens
func (gsm *GlobalStateManager) SetHelpScreensSeen(seen uint32) error {
	state, err := gsm.GetOrCreateGlobalState()
	if err != nil {
		return err
	}

	state.HelpScreensSeen = seen
	return gsm.SaveGlobalState()
}

// Ensure GlobalStateManager implements AppState interface
var _ AppState = (*GlobalStateManager)(nil)

// MigrateLegacyState migrates data from legacy state.json to new project-based structure
func (gsm *GlobalStateManager) MigrateLegacyState(legacyInstancesData json.RawMessage) error {
	// This is a simplified version that just marks migration as complete
	// The actual migration logic should be handled by the session package
	globalState, err := gsm.GetOrCreateGlobalState()
	if err != nil {
		return fmt.Errorf("failed to get global state: %w", err)
	}

	// Check if migration already happened
	if globalState.LastMigrationVersion >= 1 {
		log.InfoLog.Printf("Migration already completed (version %d)", globalState.LastMigrationVersion)
		return nil
	}

	log.InfoLog.Printf("Marking migration as completed (actual migration handled by session package)")

	// Mark migration as completed
	globalState.LastMigrationVersion = 1
	if err := gsm.SaveGlobalState(); err != nil {
		return fmt.Errorf("failed to save global state after migration: %w", err)
	}

	return nil
}

// GenerateProjectID generates a unique project ID from repository path
func GenerateProjectID(repoPath string) string {
	hash := sha256.Sum256([]byte(repoPath))
	return hex.EncodeToString(hash[:])[:16] // Use first 16 characters
}