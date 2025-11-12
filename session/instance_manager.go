package session

import (
	"claude-squad/config"
	"claude-squad/log"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/go-git/go-git/v5"
)

// ProjectInstanceManager manages instances for a specific project
type ProjectInstanceManager struct {
	projectID       string
	repoPath        string
	projectStorage  *ProjectStorage
	globalManager   *config.GlobalStateManager
}

// NewProjectInstanceManager creates a new project instance manager
func NewProjectInstanceManager(projectID, repoPath string, configDir string) *ProjectInstanceManager {
	return &ProjectInstanceManager{
		projectID:      projectID,
		repoPath:       repoPath,
		projectStorage: NewProjectStorage(configDir, projectID, repoPath),
		globalManager:  config.NewGlobalStateManager(configDir),
	}
}

// CreateInstance creates a new instance within the project
func (pm *ProjectInstanceManager) CreateInstance(opts InstanceOptions) (*Instance, error) {
	// Check instance limit
	instances, err := pm.GetAllInstances()
	if err != nil {
		return nil, fmt.Errorf("failed to load instances: %w", err)
	}

	if len(instances) >= ProjectInstanceLimit {
		return nil, fmt.Errorf("project instance limit reached: maximum %d instances allowed", ProjectInstanceLimit)
	}

	// Create new instance
	instance, err := NewInstance(opts)
	if err != nil {
		return nil, fmt.Errorf("failed to create instance: %w", err)
	}

	// Start the instance
	if err := instance.Start(true); err != nil {
		return nil, fmt.Errorf("failed to start instance: %w", err)
	}

	// Save instance to project storage
	instanceData := instance.ToInstanceData()
	if err := pm.projectStorage.AddInstance(instanceData); err != nil {
		// Clean up the instance if saving fails
		instance.Kill()
		return nil, fmt.Errorf("failed to save instance: %w", err)
	}

	// Update global state
	if err := pm.globalManager.UpdateProjectInstanceCount(pm.projectID, len(instances)+1); err != nil {
		log.WarningLog.Printf("Failed to update project instance count: %v", err)
	}

	return instance, nil
}

// GetAllInstances returns all instances for this project
func (pm *ProjectInstanceManager) GetAllInstances() ([]*Instance, error) {
	instancesData, err := pm.projectStorage.GetInstances()
	if err != nil {
		return nil, fmt.Errorf("failed to load instances data: %w", err)
	}

	instances := make([]*Instance, 0, len(instancesData))
	for _, data := range instancesData {
		instance, err := FromInstanceData(data)
		if err != nil {
			log.ErrorLog.Printf("Failed to create instance from data: %v", err)
			continue
		}
		instances = append(instances, instance)
	}

	return instances, nil
}

// GetInstance returns a specific instance by title
func (pm *ProjectInstanceManager) GetInstance(title string) (*Instance, error) {
	instances, err := pm.GetAllInstances()
	if err != nil {
		return nil, err
	}

	for _, instance := range instances {
		if instance.Title == title {
			return instance, nil
		}
	}

	return nil, fmt.Errorf("instance not found: %s", title)
}

// UpdateInstance updates an existing instance
func (pm *ProjectInstanceManager) UpdateInstance(instance *Instance) error {
	if !instance.Started() {
		return fmt.Errorf("cannot update instance that has not been started")
	}

	instanceData := instance.ToInstanceData()
	if err := pm.projectStorage.UpdateInstance(instanceData); err != nil {
		return fmt.Errorf("failed to update instance: %w", err)
	}

	return nil
}

// DeleteInstance deletes an instance from the project
func (pm *ProjectInstanceManager) DeleteInstance(title string) error {
	// Get instance to clean up resources
	instance, err := pm.GetInstance(title)
	if err != nil {
		return fmt.Errorf("failed to get instance: %w", err)
	}

	// Kill the instance
	if err := instance.Kill(); err != nil {
		log.WarningLog.Printf("Failed to kill instance during deletion: %v", err)
	}

	// Delete from storage
	if err := pm.projectStorage.DeleteInstance(title); err != nil {
		return fmt.Errorf("failed to delete instance from storage: %w", err)
	}

	// Update global state
	instances, err := pm.GetAllInstances()
	if err != nil {
		log.WarningLog.Printf("Failed to get instance count for update: %v", err)
	} else {
		if err := pm.globalManager.UpdateProjectInstanceCount(pm.projectID, len(instances)); err != nil {
			log.WarningLog.Printf("Failed to update project instance count: %v", err)
		}
	}

	return nil
}

// GetProjectData returns the project metadata
func (pm *ProjectInstanceManager) GetProjectData() (*config.GlobalProjectData, error) {
	return pm.globalManager.GetProject(pm.projectID)
}

// GetRepoPath returns the repository path for this project
func (pm *ProjectInstanceManager) GetRepoPath() string {
	return pm.repoPath
}

// GetProjectID returns the project ID
func (pm *ProjectInstanceManager) GetProjectID() string {
	return pm.projectID
}

// InstanceManager provides a high-level interface for managing instances across all projects
type InstanceManager struct {
	configDir     string
	globalManager *config.GlobalStateManager
}

// NewInstanceManager creates a new instance manager
func NewInstanceManager(configDir string) *InstanceManager {
	return &InstanceManager{
		configDir:     configDir,
		globalManager: config.NewGlobalStateManager(configDir),
	}
}

// GetProjectManager returns a project-specific instance manager
func (im *InstanceManager) GetProjectManager(projectID, repoPath string) *ProjectInstanceManager {
	return NewProjectInstanceManager(projectID, repoPath, im.configDir)
}

// GetCurrentProjectManager returns the project manager for the current working directory
func (im *InstanceManager) GetCurrentProjectManager() (*ProjectInstanceManager, error) {
	// Get current working directory
	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("failed to get current working directory: %w", err)
	}

	// Find Git repository root
	repoPath, err := findGitRepoRootFromPath(cwd)
	if err != nil {
		return nil, fmt.Errorf("failed to find Git repository root: %w", err)
	}

	// Generate project ID
	projectID := config.GenerateProjectID(repoPath)

	// Ensure project exists in global state
	projectData, err := im.globalManager.GetProject(projectID)
	if err != nil {
		return nil, fmt.Errorf("failed to get project: %w", err)
	}

	if projectData == nil {
		// Create new project
		projectName := filepath.Base(repoPath)
		if err = im.globalManager.AddProject(projectID, projectName, repoPath); err != nil {
			return nil, fmt.Errorf("failed to add project: %w", err)
		}
	}

	return im.GetProjectManager(projectID, repoPath), nil
}

// GetAllProjects returns all projects
func (im *InstanceManager) GetAllProjects() ([]config.GlobalProjectData, error) {
	return im.globalManager.GetAllProjects()
}

// MigrateLegacyState migrates from old state format
func (im *InstanceManager) MigrateLegacyState(legacyInstancesData json.RawMessage) error {
	return im.globalManager.MigrateLegacyState(legacyInstancesData)
}

// findGitRepoRootFromPath finds the Git repository root from a given path
func findGitRepoRootFromPath(path string) (string, error) {
	currentPath := path
	for {
		_, err := git.PlainOpen(currentPath)
		if err == nil {
			// Found the repository root
			return currentPath, nil
		}

		parent := filepath.Dir(currentPath)
		if parent == currentPath {
			// Reached the filesystem root without finding a repository
			return "", fmt.Errorf("failed to find Git repository root from path: %s", path)
		}
		currentPath = parent
	}
}