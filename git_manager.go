/*
Copyright 2024 Blnk Finance Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package watch

import (
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
	zlog "github.com/rs/zerolog/log"
)

type GitManager struct {
	RepoURL      string
	Branch       string
	LocalPath    string
	GitUsername  string
	GitToken     string
	PollInterval time.Duration
	watcher      *fsnotify.Watcher
	stopChan     chan bool
}

// NewGitManager creates a new GitManager instance
func NewGitManager(repoURL, branch, localPath, gitUsername, gitToken string) *GitManager {
	if branch == "" {
		branch = "main"
	}

	cleanRepoURL, embeddedUsername, embeddedToken := normalizeRepoURL(repoURL)
	if gitUsername == "" {
		gitUsername = embeddedUsername
	}
	if gitToken == "" {
		gitToken = embeddedToken
	}
	if gitUsername == "" {
		gitUsername = "x-access-token"
	}

	return &GitManager{
		RepoURL:      cleanRepoURL,
		Branch:       branch,
		LocalPath:    localPath,
		GitUsername:  gitUsername,
		GitToken:     gitToken,
		PollInterval: 30 * time.Second, // Check for updates every 30 seconds
		stopChan:     make(chan bool),
	}
}

func normalizeRepoURL(repoURL string) (string, string, string) {
	parsedURL, err := url.Parse(repoURL)
	if err != nil || parsedURL.Scheme == "" || parsedURL.User == nil {
		return repoURL, "", ""
	}

	username := parsedURL.User.Username()
	token, _ := parsedURL.User.Password()
	parsedURL.User = nil

	return parsedURL.String(), username, token
}

func gitAuthEnv(repoURL, gitUsername, gitToken string) []string {
	if gitToken == "" {
		return nil
	}

	parsedURL, err := url.Parse(repoURL)
	if err != nil {
		return nil
	}

	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return nil
	}

	if gitUsername == "" {
		gitUsername = "x-access-token"
	}

	credentialHelper := fmt.Sprintf("!f() { printf 'username=%%s\\npassword=%%s\\n' %s %s; }; f",
		shellQuote(gitUsername),
		shellQuote(gitToken),
	)

	return []string{
		"GIT_CONFIG_COUNT=1",
		"GIT_CONFIG_KEY_0=credential.helper",
		fmt.Sprintf("GIT_CONFIG_VALUE_0=%s", credentialHelper),
	}
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func gitCommand(repoURL, gitUsername, gitToken string, args ...string) *exec.Cmd {
	cmd := exec.Command("git", args...)
	if env := gitAuthEnv(repoURL, gitUsername, gitToken); len(env) > 0 {
		cmd.Env = append(filterEnv(os.Environ(), "GIT_CONFIG_COUNT", "GIT_CONFIG_KEY_0", "GIT_CONFIG_VALUE_0"), env...)
	}
	return cmd
}

func filterEnv(env []string, keys ...string) []string {
	keySet := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		keySet[key] = struct{}{}
	}

	filtered := make([]string, 0, len(env))
	for _, entry := range env {
		key, _, ok := strings.Cut(entry, "=")
		if !ok {
			filtered = append(filtered, entry)
			continue
		}
		if _, exists := keySet[key]; exists {
			continue
		}
		filtered = append(filtered, entry)
	}

	return filtered
}

// CloneOrUpdate clones the repository if it doesn't exist, or updates it if it does.
// This is the primary method to initialize or refresh the local repository.
// It handles edge cases like invalid repositories by removing and re-cloning them.
//
// Returns an error if the clone or update operation fails.
func (gm *GitManager) CloneOrUpdate() error {
	if _, err := os.Stat(gm.LocalPath); os.IsNotExist(err) {
		return gm.cloneRepository()
	}

	if !gm.isValidGitRepo() {
		zlog.Warn().Str("path", gm.LocalPath).Msg("Directory exists but is not a valid Git repository, removing and re-cloning")
		if err := os.RemoveAll(gm.LocalPath); err != nil {
			return fmt.Errorf("failed to remove invalid repository directory: %w", err)
		}
		return gm.cloneRepository()
	}

	return gm.updateRepository()
}

func (gm *GitManager) cloneRepository() error {
	zlog.Info().
		Str("repo", gm.RepoURL).
		Str("branch", gm.Branch).
		Str("path", gm.LocalPath).
		Msg("Cloning Git repository")

	parentDir := filepath.Dir(gm.LocalPath)
	if err := os.MkdirAll(parentDir, 0755); err != nil {
		return fmt.Errorf("failed to create parent directory: %w", err)
	}

	cmd := gitCommand(gm.RepoURL, gm.GitUsername, gm.GitToken, "clone", "-b", gm.Branch, gm.RepoURL, gm.LocalPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to clone repository: %w\nOutput: %s", err, string(output))
	}

	zlog.Info().
		Str("path", gm.LocalPath).
		Msg("Successfully cloned Git repository")

	return nil
}

func (gm *GitManager) isValidGitRepo() bool {
	gitDir := filepath.Join(gm.LocalPath, ".git")
	if _, err := os.Stat(gitDir); os.IsNotExist(err) {
		return false
	}

	// Change to repository directory
	originalDir, err := os.Getwd()
	if err != nil {
		return false
	}
	defer os.Chdir(originalDir)

	if err := os.Chdir(gm.LocalPath); err != nil {
		return false
	}

	cmd := exec.Command("git", "remote", "get-url", "origin")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return false
	}

	remoteURL := strings.TrimSpace(string(output))
	return remoteURL == gm.RepoURL
}

func (gm *GitManager) ensureOriginRemote() error {
	cmd := exec.Command("git", "remote", "get-url", "origin")
	output, err := cmd.CombinedOutput()

	if err != nil {
		zlog.Info().Str("repo", gm.RepoURL).Msg("Adding origin remote")
		cmd = exec.Command("git", "remote", "add", "origin", gm.RepoURL)
		if output, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("failed to add origin remote: %w\nOutput: %s", err, string(output))
		}
		return nil
	}

	currentURL := strings.TrimSpace(string(output))
	if currentURL != gm.RepoURL {
		zlog.Info().
			Str("old_url", currentURL).
			Str("new_url", gm.RepoURL).
			Msg("Updating origin remote URL")
		cmd = exec.Command("git", "remote", "set-url", "origin", gm.RepoURL)
		if output, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("failed to update origin remote: %w\nOutput: %s", err, string(output))
		}
	}

	return nil
}

func (gm *GitManager) updateRepository() error {
	originalDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}
	defer os.Chdir(originalDir)

	if err := os.Chdir(gm.LocalPath); err != nil {
		return fmt.Errorf("failed to change to repository directory: %w", err)
	}

	if err := gm.ensureOriginRemote(); err != nil {
		return fmt.Errorf("failed to ensure origin remote: %w", err)
	}

	cmd := gitCommand(gm.RepoURL, gm.GitUsername, gm.GitToken, "fetch", "origin")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to fetch from remote: %w\nOutput: %s", err, string(output))
	}

	cmd = exec.Command("git", "rev-parse", "HEAD")
	localCommitOutput, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to get local commit hash: %w", err)
	}
	localCommit := strings.TrimSpace(string(localCommitOutput))

	cmd = exec.Command("git", "rev-parse", fmt.Sprintf("origin/%s", gm.Branch))
	remoteCommitOutput, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to get remote commit hash: %w", err)
	}
	remoteCommit := strings.TrimSpace(string(remoteCommitOutput))

	if localCommit == remoteCommit {
		return nil
	}

	if err := gm.handleLocalChanges(); err != nil {
		zlog.Warn().Err(err).Msg("Failed to handle local changes, attempting to continue")
	}

	cmd = gitCommand(gm.RepoURL, gm.GitUsername, gm.GitToken, "pull", "origin", gm.Branch)
	output, err = cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to pull changes: %w\nOutput: %s", err, string(output))
	}

	zlog.Info().
		Str("from", localCommit[:7]).
		Str("to", remoteCommit[:7]).
		Msg("Successfully updated Git repository")

	return nil
}

// handleLocalChanges deals with local modifications before pulling
func (gm *GitManager) handleLocalChanges() error {
	// Check if there are any local changes
	cmd := exec.Command("git", "status", "--porcelain")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to check git status: %w", err)
	}

	changes := strings.TrimSpace(string(output))
	if changes == "" {
		// No local changes
		return nil
	}

	zlog.Info().Str("changes", changes).Msg("Detected local changes, resetting to match remote")

	// Reset any uncommitted changes
	cmd = exec.Command("git", "reset", "--hard", "HEAD")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to reset local changes: %w\nOutput: %s", err, string(output))
	}

	cmd = exec.Command("git", "clean", "-fd")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to clean untracked files: %w\nOutput: %s", err, string(output))
	}

	zlog.Info().Msg("Successfully reset local changes")
	return nil
}

func (gm *GitManager) StartPeriodicSync() {
	go func() {
		ticker := time.NewTicker(gm.PollInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				if err := gm.updateRepository(); err != nil {
					zlog.Error().Err(err).Msg("Failed to update Git repository")
				} else {
					go processExistingScriptsInDir(gm.LocalPath)
				}
			case <-gm.stopChan:
				zlog.Info().Msg("Stopping Git repository sync")
				return
			}
		}
	}()
}

func (gm *GitManager) StartWatching() error {
	var err error
	gm.watcher, err = fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("failed to create file watcher: %w", err)
	}

	err = gm.watcher.Add(gm.LocalPath)
	if err != nil {
		return fmt.Errorf("failed to watch repository directory: %w", err)
	}

	go func() {
		defer gm.watcher.Close()

		for {
			select {
			case event, ok := <-gm.watcher.Events:
				if !ok {
					return
				}

				if strings.HasSuffix(event.Name, ".ws") {
					zlog.Info().
						Str("file", event.Name).
						Str("operation", event.Op.String()).
						Msg("Watch script file changed")

					if event.Op&fsnotify.Write == fsnotify.Write || event.Op&fsnotify.Create == fsnotify.Create {
						go processScriptFile(event.Name)
					}
				}

			case err, ok := <-gm.watcher.Errors:
				if !ok {
					return
				}
				zlog.Error().Err(err).Msg("File watcher error")

			case <-gm.stopChan:
				return
			}
		}
	}()

	return nil
}

func (gm *GitManager) Stop() {
	close(gm.stopChan)
	if gm.watcher != nil {
		gm.watcher.Close()
	}
}

func (gm *GitManager) GetCurrentCommit() (string, error) {
	originalDir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("failed to get current directory: %w", err)
	}
	defer os.Chdir(originalDir)

	if err := os.Chdir(gm.LocalPath); err != nil {
		return "", fmt.Errorf("failed to change to repository directory: %w", err)
	}

	cmd := exec.Command("git", "rev-parse", "HEAD")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to get commit hash: %w", err)
	}

	return strings.TrimSpace(string(output)), nil
}

func (gm *GitManager) GetRemoteCommit() (string, error) {
	originalDir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("failed to get current directory: %w", err)
	}
	defer os.Chdir(originalDir)

	if err := os.Chdir(gm.LocalPath); err != nil {
		return "", fmt.Errorf("failed to change to repository directory: %w", err)
	}

	// Fetch latest changes first
	cmd := gitCommand(gm.RepoURL, gm.GitUsername, gm.GitToken, "fetch", "origin")
	if _, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("failed to fetch from remote: %w", err)
	}

	cmd = exec.Command("git", "rev-parse", fmt.Sprintf("origin/%s", gm.Branch))
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to get remote commit hash: %w", err)
	}

	return strings.TrimSpace(string(output)), nil
}

func IsGitInstalled() bool {
	_, err := exec.LookPath("git")
	return err == nil
}

func ValidateGitRepo(repoURL, gitUsername, gitToken string) error {
	cleanRepoURL, embeddedUsername, embeddedToken := normalizeRepoURL(repoURL)
	if gitUsername == "" {
		gitUsername = embeddedUsername
	}
	if gitToken == "" {
		gitToken = embeddedToken
	}

	cmd := gitCommand(cleanRepoURL, gitUsername, gitToken, "ls-remote", "--heads", cleanRepoURL)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("invalid Git repository URL: %w\nOutput: %s", err, string(output))
	}
	return nil
}

func (gm *GitManager) GetRepositoryInfo() map[string]interface{} {
	info := map[string]interface{}{
		"repo_url":   gm.RepoURL,
		"branch":     gm.Branch,
		"local_path": gm.LocalPath,
		"exists":     false,
		"valid_git":  false,
	}

	if _, err := os.Stat(gm.LocalPath); err == nil {
		info["exists"] = true
		info["valid_git"] = gm.isValidGitRepo()

		if gm.isValidGitRepo() {
			info["git_status"] = gm.getGitStatus()
		}
	}

	// Get current commit if possible
	if commit, err := gm.GetCurrentCommit(); err == nil {
		info["current_commit"] = commit
	}

	if commit, err := gm.GetRemoteCommit(); err == nil {
		info["remote_commit"] = commit
	}

	return info
}

// getGitStatus returns detailed Git status information
func (gm *GitManager) getGitStatus() map[string]interface{} {
	status := map[string]interface{}{}

	// Change to repository directory
	originalDir, _ := os.Getwd()
	defer os.Chdir(originalDir)

	if err := os.Chdir(gm.LocalPath); err != nil {
		status["error"] = "cannot access repository directory"
		return status
	}

	if cmd := exec.Command("git", "status", "--porcelain"); cmd != nil {
		if output, err := cmd.CombinedOutput(); err == nil {
			changes := strings.TrimSpace(string(output))
			status["has_changes"] = changes != ""
			status["changes"] = changes
		}
	}

	// Get remote info
	if cmd := exec.Command("git", "remote", "-v"); cmd != nil {
		if output, err := cmd.CombinedOutput(); err == nil {
			status["remotes"] = strings.TrimSpace(string(output))
		}
	}

	if cmd := exec.Command("git", "branch", "-vv"); cmd != nil {
		if output, err := cmd.CombinedOutput(); err == nil {
			status["branches"] = strings.TrimSpace(string(output))
		}
	}

	return status
}
