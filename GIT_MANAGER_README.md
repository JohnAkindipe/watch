# GitManager Documentation

## Overview

**GitManager** is a core component of Blnk Watch that enables centralized management of watch scripts through Git repositories. It allows teams to version control, collaborate on, and deploy watch scripts (`.ws` files) from a shared Git repository, ensuring consistency across environments and enabling automated updates.

## Why GitManager Exists

Blnk Watch scripts define transaction monitoring rules, fraud detection patterns, and compliance checks. Managing these scripts individually across multiple instances or environments can lead to:

- **Inconsistency**: Different versions of rules running in different environments
- **Deployment complexity**: Manual file copying and updates
- **Collaboration challenges**: Multiple team members editing scripts without version control
- **Lack of audit trail**: No history of rule changes

GitManager solves these problems by:

1. **Centralized Management**: Store all watch scripts in a single Git repository
2. **Version Control**: Track changes, rollbacks, and maintain history
3. **Automated Deployment**: Automatically sync and deploy scripts from the repository
4. **Team Collaboration**: Multiple developers can work on scripts with proper version control
5. **Environment Consistency**: Ensure all instances run the same version of rules

## How It Fits Into Blnk Watch

GitManager integrates seamlessly into the Blnk Watch service lifecycle:

```
┌─────────────────────────────────────────────────────────────┐
│                    Blnk Watch Service                        │
├─────────────────────────────────────────────────────────────┤
│                                                               │
│  ┌──────────────┐         ┌──────────────┐                 │
│  │ GitManager   │─────────▶│ Watch Script │                 │
│  │              │  Syncs   │  Processor   │                 │
│  └──────────────┘          └──────────────┘                 │
│         │                           │                        │
│         │                           ▼                        │
│         │                  ┌──────────────┐                 │
│         │                  │ Instructions │                 │
│         │                  │   Database   │                 │
│         │                  └──────────────┘                 │
│         │                                                   │
│         ▼                                                   │
│  ┌──────────────┐                                          │
│  │ Git Repository│                                          │
│  │ (Remote)      │                                          │
│  └──────────────┘                                          │
│                                                               │
└─────────────────────────────────────────────────────────────┘
```

### Integration Points

1. **Service Startup**: When Blnk Watch starts, if `WATCH_SCRIPT_GIT_REPO` is configured, GitManager:
   - Clones or updates the repository
   - Processes all existing `.ws` files
   - Starts monitoring for changes

2. **Change Detection**: GitManager uses two mechanisms:
   - **Periodic Sync**: Polls the remote repository every 30 seconds for updates
   - **File Watching**: Monitors the local repository directory for immediate file changes

3. **Script Processing**: When changes are detected:
   - New or modified `.ws` files are automatically compiled
   - Instructions are created or updated in the database
   - Changes take effect immediately without service restart

## Configuration

GitManager is configured through environment variables:

### Required Configuration

```bash
# Git repository URL containing watch scripts
WATCH_SCRIPT_GIT_REPO=https://github.com/your-org/watch-scripts.git
```

### Optional Configuration

```bash
# Local directory to clone the repository (default: "watch_scripts")
WATCH_SCRIPT_DIR=watch_scripts

# Git branch to track (default: "main")
WATCH_SCRIPT_GIT_BRANCH=main

# Optional credentials for private HTTPS repositories
# Username defaults to "x-access-token" if unset
WATCH_SCRIPT_GIT_USERNAME=
WATCH_SCRIPT_GIT_TOKEN=
```

### Example Configuration

```bash
# .env file
WATCH_SCRIPT_GIT_REPO=https://github.com/blnk-finance/production-rules.git
WATCH_SCRIPT_GIT_BRANCH=production
WATCH_SCRIPT_DIR=/var/lib/blnk-watch/scripts
WATCH_SCRIPT_GIT_USERNAME=x-access-token
WATCH_SCRIPT_GIT_TOKEN=ghp_your_token_here
```

For private repositories over HTTPS, the app rewrites the Git URL at command
time and keeps the configured repo URL clean in logs and status responses.
SSH works as long as the runtime has the right key loaded.

## How It Works

### Initial Setup

1. **Repository Validation**: On startup, GitManager validates the Git repository URL
2. **Clone or Update**: 
   - If the local directory doesn't exist, it clones the repository
   - If it exists, it validates it's a proper Git repo and updates it
3. **Initial Processing**: All `.ws` files in the repository are processed and loaded
4. **Monitoring Start**: Both periodic sync and file watching are started

### Change Detection

#### Periodic Sync (Default: 30 seconds)

- Compares local and remote commit hashes
- If different, pulls latest changes
- Resets any local modifications to match remote
- Processes all scripts in the directory

#### File System Watching

- Monitors the local repository directory for file system events
- Detects `.ws` file creation and modification
- Immediately processes changed files
- Provides near-instantaneous updates

### Local Change Handling

GitManager maintains a "source of truth" approach:

- **Local changes are automatically reset** to match the remote repository
- This ensures all instances always run the exact same version
- Prevents configuration drift between environments
- Changes should be made in the Git repository, not locally

### Repository Structure

The Git repository should contain `.ws` files in any directory structure:

```
watch-scripts/
├── fraud-detection/
│   ├── high-value-transactions.ws
│   ├── rapid-transactions.ws
│   └── suspicious-patterns.ws
├── compliance/
│   ├── kyc-checks.ws
│   └── sanction-checks.ws
└── limits/
    ├── daily-limits.ws
    └── velocity-limits.ws
```

GitManager will recursively process all `.ws` files found in the repository.

## Features

### 1. Automatic Synchronization

- Periodically checks for updates from the remote repository
- Automatically pulls and applies changes
- No manual intervention required

### 2. Dual Monitoring

- **Periodic Sync**: Ensures eventual consistency with remote
- **File Watching**: Provides immediate local change detection
- Both mechanisms work together for comprehensive coverage

### 3. Repository Validation

- Validates Git repository URL before cloning
- Checks repository integrity on startup
- Handles corrupted repositories by re-cloning

### 4. Error Handling

- Gracefully handles network failures
- Logs errors without crashing the service
- Continues monitoring even after temporary failures

### 5. Manual Sync API

- Provides HTTP endpoint for manual synchronization
- Useful for triggering immediate updates
- Returns sync status and commit information

## API Endpoints

GitManager exposes HTTP endpoints for monitoring and control:

### GET /git/status

Retrieves the current status of the Git repository.

**Response Example**:
```json
{
  "configured": true,
  "repo_url": "https://github.com/user/watch-scripts.git",
  "branch": "main",
  "local_path": "watch_scripts",
  "current_commit": "abc123def456",
  "remote_commit": "abc123def456",
  "up_to_date": true,
  "last_sync": "2024-01-15T10:00:00Z"
}
```

### POST /git/sync

Manually triggers a repository synchronization.

**Response Example**:
```json
{
  "success": true,
  "message": "Repository synced and scripts reloaded",
  "before_commit": "abc123def456",
  "after_commit": "def456ghi789"
}
```

## Use Cases

### 1. Multi-Environment Deployment

Deploy the same watch scripts across development, staging, and production:

```bash
# Development
WATCH_SCRIPT_GIT_REPO=https://github.com/org/rules.git
WATCH_SCRIPT_GIT_BRANCH=develop

# Production
WATCH_SCRIPT_GIT_REPO=https://github.com/org/rules.git
WATCH_SCRIPT_GIT_BRANCH=main
```

### 2. Team Collaboration

Multiple team members can:
- Create feature branches for new rules
- Review changes through pull requests
- Merge to main branch for automatic deployment
- Roll back changes if needed

### 3. Compliance and Audit

- All rule changes are tracked in Git history
- Easy to see who changed what and when
- Can revert to previous versions if needed
- Maintains audit trail for compliance

### 4. CI/CD Integration

- Automatically deploy rules when merged to main
- Run tests on watch scripts before deployment
- Use Git tags for versioning and releases

## Best Practices

### 1. Repository Organization

- Organize scripts by category (fraud, compliance, limits)
- Use descriptive file names
- Include documentation in the repository

### 2. Branch Strategy

- Use `main` or `master` for production rules
- Create feature branches for new rules
- Use environment-specific branches if needed

### 3. Testing

- Test scripts in a development environment first
- Use pull requests for code review
- Validate scripts before merging

### 4. Monitoring

- Monitor Git sync status via API
- Set up alerts for sync failures
- Review logs for processing errors

### 5. Security

- Use private repositories for sensitive rules
- Implement proper access controls
- Use SSH keys or tokens for authentication
- Rotate credentials regularly

## Troubleshooting

### Repository Not Syncing

1. Check Git is installed: `git --version`
2. Verify repository URL is accessible
3. Check network connectivity
4. Review logs for specific error messages

### Scripts Not Processing

1. Verify `.ws` files are in the repository
2. Check file permissions
3. Review compilation errors in logs
4. Ensure scripts are valid DSL syntax

### Local Changes Being Reset

This is expected behavior. GitManager resets local changes to match the remote repository. Make changes in the Git repository instead.

### Sync Failures

- Check repository URL and branch name
- Verify Git credentials if using private repos
- Ensure sufficient disk space
- Check for repository corruption

## Limitations

1. **Git Required**: Git must be installed on the system
2. **Network Dependency**: Requires network access to the Git repository
3. **Local Changes Reset**: Local modifications are automatically reset
4. **Single Repository**: One Git repository per Blnk Watch instance
5. **No Submodules**: Git submodules are not supported

## Future Enhancements

Potential improvements:
- Support for multiple repositories
- Git submodule support
- Configurable sync intervals
- Webhook-based updates
- Repository authentication via SSH keys
- Support for Git tags and releases
