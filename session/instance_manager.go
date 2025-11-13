package session

import (
	"claude-squad/config"
	"claude-squad/log"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

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
	// Set the project ID
	opts.ProjectID = pm.projectID

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
	log.InfoLog.Printf("[PROJECT-MANAGER] GetAllInstances called for project %s", pm.projectID)

	instancesData, err := pm.projectStorage.GetInstances()
	if err != nil {
		log.ErrorLog.Printf("[PROJECT-MANAGER] Failed to load instances data: %v", err)
		return nil, fmt.Errorf("failed to load instances data: %w", err)
	}

	log.InfoLog.Printf("[PROJECT-MANAGER] Loaded %d instances data entries", len(instancesData))

	instances := make([]*Instance, 0, len(instancesData))
	for i, data := range instancesData {
		log.InfoLog.Printf("[PROJECT-MANAGER] Processing instance %d: %s", i, data.Title)
		instance, err := FromInstanceData(data)
		if err != nil {
			log.ErrorLog.Printf("[PROJECT-MANAGER] Failed to create instance from data: %v", err)
			continue
		}
		instances = append(instances, instance)
		log.InfoLog.Printf("[PROJECT-MANAGER] Successfully created instance: %s", instance.Title)
	}

	log.InfoLog.Printf("[PROJECT-MANAGER] Returning %d instances for project %s", len(instances), pm.projectID)
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
	log.InfoLog.Printf("[PROJECT] Starting GetCurrentProjectManager...")

	// Get current working directory
	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("failed to get current working directory: %w", err)
	}
	log.InfoLog.Printf("[PROJECT] Current working directory: %s", cwd)

	// Find Git repository root
	repoPath, err := findGitRepoRootFromPath(cwd)
	if err != nil {
		return nil, fmt.Errorf("failed to find Git repository root: %w", err)
	}
	log.InfoLog.Printf("[PROJECT] Found Git repository root: %s", repoPath)

	// Generate project ID
	projectID := config.GenerateProjectID(repoPath)
	log.InfoLog.Printf("[PROJECT] Generated project ID: %s", projectID)

	// Check if project already exists in global state
	projectData, err := im.globalManager.GetProject(projectID)
	if err != nil {
		return nil, fmt.Errorf("failed to get project: %w", err)
	}

	if projectData == nil {
		log.InfoLog.Printf("[PROJECT] Project not found in global state, creating new project")
		// Create new project
		projectName := filepath.Base(repoPath)
		if err = im.globalManager.AddProject(projectID, projectName, repoPath); err != nil {
			return nil, fmt.Errorf("failed to add project: %w", err)
		}
		log.InfoLog.Printf("[PROJECT] Created new project: %s (%s)", projectName, projectID)
	} else {
		log.InfoLog.Printf("[PROJECT] Found existing project: %s (%s)", projectData.Name, projectID)
		log.InfoLog.Printf("[PROJECT] Project repo path: %s", projectData.RepoPath)
		log.InfoLog.Printf("[PROJECT] Project instance count: %d", projectData.InstanceCount)
	}

	// Create project manager
	projectManager := im.GetProjectManager(projectID, repoPath)
	log.InfoLog.Printf("[PROJECT] Created project manager for project %s", projectID)

	// Test loading instances to verify project state
	instances, err := projectManager.GetAllInstances()
	if err != nil {
		log.InfoLog.Printf("[PROJECT] Warning: failed to load instances for project %s: %v", projectID, err)
	} else {
		log.InfoLog.Printf("[PROJECT] Successfully loaded %d instances for project %s", len(instances), projectID)
	}

	return projectManager, nil
}

// GetAllProjects returns all projects
func (im *InstanceManager) GetAllProjects() ([]config.GlobalProjectData, error) {
	return im.globalManager.GetAllProjects()
}

// MigrateLegacyState migrates from old state format
func (im *InstanceManager) MigrateLegacyState(legacyInstancesData json.RawMessage) error {
	log.InfoLog.Printf("[MIGRATION] Starting legacy state migration...")

	// Parse legacy instances
	type LegacyInstanceData struct {
		Title       string    `json:"title"`
		DisplayName string    `json:"display_name"`
		Path        string    `json:"path"`
		Branch      string    `json:"branch"`
		Status      int       `json:"status"`
		Height      int       `json:"height"`
		Width       int       `json:"width"`
		CreatedAt   time.Time `json:"created_at"`
		UpdatedAt   time.Time `json:"updated_at"`
		AutoYes     bool      `json:"auto_yes"`
		Program     string    `json:"program"`
		Worktree    struct {
			RepoPath      string `json:"repo_path"`
			WorktreePath  string `json:"worktree_path"`
			SessionName   string `json:"session_name"`
			BranchName    string `json:"branch_name"`
			BaseCommitSHA string `json:"base_commit_sha"`
		} `json:"worktree"`
		DiffStats struct {
			Added   int    `json:"added"`
			Removed int    `json:"removed"`
			Content string `json:"content"`
		} `json:"diff_stats"`
	}

	var legacyInstances []LegacyInstanceData
	if err := json.Unmarshal(legacyInstancesData, &legacyInstances); err != nil {
		return fmt.Errorf("failed to parse legacy instances: %w", err)
	}

	if len(legacyInstances) == 0 {
		log.InfoLog.Printf("[MIGRATION] No instances to migrate")
		return im.globalManager.MigrateLegacyState(legacyInstancesData)
	}

	log.InfoLog.Printf("[MIGRATION] Migrating %d instances...", len(legacyInstances))

	// Group instances by repository path
	projectsByRepo := make(map[string][]LegacyInstanceData)
	for _, instance := range legacyInstances {
		repoPath := instance.Worktree.RepoPath
		if repoPath == "" {
			// Fallback to instance path if no repo path
			repoPath = instance.Path
		}
		projectsByRepo[repoPath] = append(projectsByRepo[repoPath], instance)
	}

	// Create project storage for each repository
	for repoPath, instances := range projectsByRepo {
		projectID := config.GenerateProjectID(repoPath)
		projectName := filepath.Base(repoPath)

		log.InfoLog.Printf("[MIGRATION] Processing project %s (%s) with %d instances", projectName, projectID, len(instances))

		// Add project to global state
		if err := im.globalManager.AddProject(projectID, projectName, repoPath); err != nil {
			log.ErrorLog.Printf("[MIGRATION] Failed to add project %s: %v", projectID, err)
			continue
		}

		// Create project storage
		projectStorage := NewProjectStorage(im.configDir, projectID, repoPath)
		if err := projectStorage.EnsureProjectDir(); err != nil {
			log.ErrorLog.Printf("[MIGRATION] Failed to create project directory for %s: %v", projectID, err)
			continue
		}

		// Convert legacy instances to current format
		currentInstances := make([]InstanceData, len(instances))
		for i, legacyInstance := range instances {
			currentInstances[i] = InstanceData{
				Title:       legacyInstance.Title,
				DisplayName: legacyInstance.DisplayName,
				Path:        legacyInstance.Path,
				Branch:      legacyInstance.Branch,
				Status:      Status(legacyInstance.Status),
				Height:      legacyInstance.Height,
				Width:       legacyInstance.Width,
				CreatedAt:   legacyInstance.CreatedAt,
				UpdatedAt:   legacyInstance.UpdatedAt,
				AutoYes:     legacyInstance.AutoYes,
				Program:     legacyInstance.Program,
				Worktree: GitWorktreeData{
					RepoPath:      legacyInstance.Worktree.RepoPath,
					WorktreePath:  legacyInstance.Worktree.WorktreePath,
					SessionName:   legacyInstance.Worktree.SessionName,
					BranchName:    legacyInstance.Worktree.BranchName,
					BaseCommitSHA: legacyInstance.Worktree.BaseCommitSHA,
				},
				DiffStats: DiffStatsData{
					Added:   legacyInstance.DiffStats.Added,
					Removed: legacyInstance.DiffStats.Removed,
					Content: legacyInstance.DiffStats.Content,
				},
			}
		}

		// Save instances to project storage
		if err := projectStorage.SaveInstances(currentInstances); err != nil {
			log.ErrorLog.Printf("[MIGRATION] Failed to save instances for project %s: %v", projectID, err)
			continue
		}

		// Update instance count
		if err := im.globalManager.UpdateProjectInstanceCount(projectID, len(instances)); err != nil {
			log.ErrorLog.Printf("[MIGRATION] Failed to update instance count for project %s: %v", projectID, err)
			continue
		}

		log.InfoLog.Printf("[MIGRATION] Successfully migrated %d instances to project %s (%s)", len(instances), projectName, projectID)
	}

	log.InfoLog.Printf("[MIGRATION] Migration completed successfully")
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