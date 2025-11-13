package session

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
	ProjectsDirName     = "projects"
	ProjectStateFileName = "state.json"
	ProjectWorktreesDirName = "worktrees"
	ProjectInstanceLimit = 10
)

// ProjectData represents a project's metadata
type ProjectData struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	RepoPath    string    `json:"repo_path"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	InstanceCount int     `json:"instance_count"`
}

// ProjectState represents the state of a single project
type ProjectState struct {
	Project  ProjectData   `json:"project"`
	Instances []InstanceData `json:"instances"`
}

// GlobalState represents the global application state
type GlobalState struct {
	Projects      []ProjectData `json:"projects"`
	HelpScreensSeen uint32      `json:"help_screens_seen"`
}

// ProjectStorage handles project-specific storage operations
type ProjectStorage struct {
	configDir string
	projectID string
	repoPath  string
}

// NewProjectStorage creates a new project storage instance
func NewProjectStorage(configDir, projectID, repoPath string) *ProjectStorage {
	return &ProjectStorage{
		configDir: configDir,
		projectID: projectID,
		repoPath:  repoPath,
	}
}

// GenerateProjectID generates a unique project ID from repository path
func GenerateProjectID(repoPath string) string {
	hash := sha256.Sum256([]byte(repoPath))
	return hex.EncodeToString(hash[:])[:16] // Use first 16 characters
}

// GetProjectDir returns the directory path for this project
func (ps *ProjectStorage) GetProjectDir() string {
	return filepath.Join(ps.configDir, ProjectsDirName, ps.projectID)
}

// GetProjectStatePath returns the path to the project state file
func (ps *ProjectStorage) GetProjectStatePath() string {
	return filepath.Join(ps.GetProjectDir(), ProjectStateFileName)
}

// GetProjectWorktreesDir returns the worktrees directory for this project
func (ps *ProjectStorage) GetProjectWorktreesDir() string {
	return filepath.Join(ps.GetProjectDir(), ProjectWorktreesDirName)
}

// EnsureProjectDir creates the project directory structure if it doesn't exist
func (ps *ProjectStorage) EnsureProjectDir() error {
	projectDir := ps.GetProjectDir()
	worktreesDir := ps.GetProjectWorktreesDir()

	if err := os.MkdirAll(projectDir, 0755); err != nil {
		return fmt.Errorf("failed to create project directory %s: %w", projectDir, err)
	}

	if err := os.MkdirAll(worktreesDir, 0755); err != nil {
		return fmt.Errorf("failed to create worktrees directory %s: %w", worktreesDir, err)
	}

	return nil
}

// LoadProjectState loads the project state from disk
func (ps *ProjectStorage) LoadProjectState() (*ProjectState, error) {
	log.InfoLog.Printf("[PROJECT-STORAGE] Loading project state for project %s", ps.projectID)

	statePath := ps.GetProjectStatePath()
	log.InfoLog.Printf("[PROJECT-STORAGE] State file path: %s", statePath)

	data, err := os.ReadFile(statePath)
	if err != nil {
		if os.IsNotExist(err) {
			log.InfoLog.Printf("[PROJECT-STORAGE] State file not found, returning default state")
			// Return default state if file doesn't exist
			return ps.DefaultProjectState(), nil
		}
		return nil, fmt.Errorf("failed to read project state: %w", err)
	}

	log.InfoLog.Printf("[PROJECT-STORAGE] Successfully read state file, size: %d bytes", len(data))

	var state ProjectState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("failed to parse project state: %w", err)
	}

	log.InfoLog.Printf("[PROJECT-STORAGE] Parsed state: Project=%s, Instances=%d",
		state.Project.Name, len(state.Instances))

	return &state, nil
}

// SaveProjectState saves the project state to disk
func (ps *ProjectStorage) SaveProjectState(state *ProjectState) error {
	log.InfoLog.Printf("[PROJECT-STORAGE] SaveProjectState called for project %s", ps.projectID)

	if err := ps.EnsureProjectDir(); err != nil {
		return err
	}

	statePath := ps.GetProjectStatePath()
	log.InfoLog.Printf("[PROJECT-STORAGE] Saving to path: %s", statePath)

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal project state: %w", err)
	}

	log.InfoLog.Printf("[PROJECT-STORAGE] Writing %d bytes to state file", len(data))

	if err := os.WriteFile(statePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write project state: %w", err)
	}

	log.InfoLog.Printf("[PROJECT-STORAGE] Successfully saved project state")
	return nil
}

// DefaultProjectState returns the default project state
func (ps *ProjectStorage) DefaultProjectState() *ProjectState {
	projectName := filepath.Base(ps.repoPath)
	return &ProjectState{
		Project: ProjectData{
			ID:          ps.projectID,
			Name:        projectName,
			RepoPath:    ps.repoPath,
			CreatedAt:   time.Now(),
			UpdatedAt:   time.Now(),
			InstanceCount: 0,
		},
		Instances: []InstanceData{},
	}
}

// GetInstances loads all instances for this project
func (ps *ProjectStorage) GetInstances() ([]InstanceData, error) {
	log.InfoLog.Printf("[PROJECT-STORAGE] GetInstances called for project %s", ps.projectID)

	state, err := ps.LoadProjectState()
	if err != nil {
		log.InfoLog.Printf("[PROJECT-STORAGE] Failed to load project state: %v", err)
		return nil, err
	}

	log.InfoLog.Printf("[PROJECT-STORAGE] Returning %d instances for project %s", len(state.Instances), ps.projectID)
	return state.Instances, nil
}

// SaveInstances saves all instances for this project
func (ps *ProjectStorage) SaveInstances(instances []InstanceData) error {
	log.InfoLog.Printf("[PROJECT-STORAGE] SaveInstances called for project %s with %d instances", ps.projectID, len(instances))

	state, err := ps.LoadProjectState()
	if err != nil {
		log.InfoLog.Printf("[PROJECT-STORAGE] Failed to load project state: %v", err)
		return err
	}

	state.Instances = instances
	state.Project.InstanceCount = len(instances)
	state.Project.UpdatedAt = time.Now()

	log.InfoLog.Printf("[PROJECT-STORAGE] Saving %d instances for project %s", len(instances), ps.projectID)
	return ps.SaveProjectState(state)
}

// AddInstance adds a new instance to the project
func (ps *ProjectStorage) AddInstance(instance InstanceData) error {
	instances, err := ps.GetInstances()
	if err != nil {
		return err
	}

	// Check instance limit
	if len(instances) >= ProjectInstanceLimit {
		return fmt.Errorf("project instance limit reached: maximum %d instances allowed", ProjectInstanceLimit)
	}

	// Check for duplicate title
	for _, existing := range instances {
		if existing.Title == instance.Title {
			return fmt.Errorf("instance with title '%s' already exists", instance.Title)
		}
	}

	instances = append(instances, instance)
	return ps.SaveInstances(instances)
}

// UpdateInstance updates an existing instance
func (ps *ProjectStorage) UpdateInstance(instance InstanceData) error {
	instances, err := ps.GetInstances()
	if err != nil {
		return err
	}

	found := false
	for i, existing := range instances {
		if existing.Title == instance.Title {
			instances[i] = instance
			found = true
			break
		}
	}

	if !found {
		return fmt.Errorf("instance not found: %s", instance.Title)
	}

	return ps.SaveInstances(instances)
}

// DeleteInstance removes an instance from the project
func (ps *ProjectStorage) DeleteInstance(title string) error {
	instances, err := ps.GetInstances()
	if err != nil {
		return err
	}

	found := false
	newInstances := make([]InstanceData, 0)
	for _, instance := range instances {
		if instance.Title != title {
			newInstances = append(newInstances, instance)
		} else {
			found = true
		}
	}

	if !found {
		return fmt.Errorf("instance not found: %s", title)
	}

	return ps.SaveInstances(newInstances)
}

// DeleteAllInstances removes all instances from the project
func (ps *ProjectStorage) DeleteAllInstances() error {
	return ps.SaveInstances([]InstanceData{})
}

// GetProjectData returns the project metadata
func (ps *ProjectStorage) GetProjectData() (*ProjectData, error) {
	state, err := ps.LoadProjectState()
	if err != nil {
		return nil, err
	}
	return &state.Project, nil
}