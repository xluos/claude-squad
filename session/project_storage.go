package session

import (
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
	statePath := ps.GetProjectStatePath()
	data, err := os.ReadFile(statePath)
	if err != nil {
		if os.IsNotExist(err) {
			// Return default state if file doesn't exist
			return ps.DefaultProjectState(), nil
		}
		return nil, fmt.Errorf("failed to read project state: %w", err)
	}

	var state ProjectState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("failed to parse project state: %w", err)
	}

	return &state, nil
}

// SaveProjectState saves the project state to disk
func (ps *ProjectStorage) SaveProjectState(state *ProjectState) error {
	if err := ps.EnsureProjectDir(); err != nil {
		return err
	}

	statePath := ps.GetProjectStatePath()
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal project state: %w", err)
	}

	if err := os.WriteFile(statePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write project state: %w", err)
	}

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
	state, err := ps.LoadProjectState()
	if err != nil {
		return nil, err
	}
	return state.Instances, nil
}

// SaveInstances saves all instances for this project
func (ps *ProjectStorage) SaveInstances(instances []InstanceData) error {
	state, err := ps.LoadProjectState()
	if err != nil {
		return err
	}

	state.Instances = instances
	state.Project.InstanceCount = len(instances)
	state.Project.UpdatedAt = time.Now()

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