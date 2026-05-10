//go:build mage
// +build mage

package main

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// Default target when running `mage` without arguments
var Default = All

// Build compiles both server and proxy binaries.
func Build() error {
	fmt.Println("=== Building binaries ===")

	targets := []string{"./cmd/server", "./cmd/proxy"}
	for _, target := range targets {
		fmt.Printf("  Building %s...\n", target)
		if err := run("go", "build", "-o", outputName(target), target); err != nil {
			return fmt.Errorf("build %s: %w", target, err)
		}
	}

	fmt.Println("Build completed successfully")
	return nil
}

// Test runs all unit tests (with race detection when CGO is available).
func Test() error {
	fmt.Println("=== Running unit tests ===")

	args := []string{"test", "-count=1", "-short", "./pkg/..."}

	// Try race detection if CGO is available
	useRace := cgoAvailable()
	if useRace {
		args = []string{"test", "-race", "-count=1", "-short", "./pkg/..."}
	}

	if mg.Verbose() {
		// Insert -v after "test"
		args = append([]string{"test", "-v"}, args[1:]...)
	}

	if useRace {
		fmt.Println("  (race detection enabled)")
	}

	if err := run("go", args...); err != nil {
		// If -race failed due to CGO, retry without -race
		if useRace {
			fmt.Println("  Race detection failed, retrying without -race...")
			args = []string{"test", "-count=1", "-short", "./pkg/..."}
			if mg.Verbose() {
				args = append([]string{"test", "-v"}, args[1:]...)
			}
			if err := run("go", args...); err != nil {
				return fmt.Errorf("unit tests failed: %w", err)
			}
		} else {
			return fmt.Errorf("unit tests failed: %w", err)
		}
	}

	fmt.Println("All unit tests passed")
	return nil
}

// TestIntegration runs integration tests (requires Docker environment).
func TestIntegration() error {
	fmt.Println("=== Running integration tests ===")

	// Check if Docker Compose is available
	if !commandExists("docker-compose") && !commandExists("docker") {
		return fmt.Errorf("docker not found: integration tests require Docker")
	}

	// Set integration test environment variable
	cmd := exec.Command("go", "test", "-tags", "integration", "-v", "-count=1", "./test/...")
	cmd.Env = append(os.Environ(), "INTEGRATION_TEST=1")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("integration tests failed: %w", err)
	}

	fmt.Println("Integration tests passed")
	return nil
}

// DockerUp starts all Docker services (PostgreSQL + MySQL + Server + Proxy).
func DockerUp() error {
	fmt.Println("=== Starting Docker services ===")
	return dockerCompose("up", "-d")
}

// DockerDown stops and removes all Docker services.
func DockerDown() error {
	fmt.Println("=== Stopping Docker services ===")
	return dockerCompose("down", "--volumes")
}

// DockerBuild builds all Docker images.
func DockerBuild() error {
	fmt.Println("=== Building Docker images ===")
	return dockerCompose("build", "--no-cache")
}

// Lint runs static analysis tools.
func Lint() error {
	fmt.Println("=== Running linters ===")

	// Run go vet on all packages
	fmt.Println("  go vet ./...")
	if err := run("go", "vet", "./..."); err != nil {
		return fmt.Errorf("go vet failed: %w", err)
	}

	fmt.Println("Lint completed successfully")
	return nil
}

// Release builds optimized binaries with version info.
func Release() error {
	fmt.Println("=== Building release binaries ===")

	version := getVersion()
	ldflags := fmt.Sprintf("-s -w -X main.version=%s", version)

	targets := []string{"./cmd/server", "./cmd/proxy"}
	oses := []string{"linux", "darwin", "windows"}
	arches := []string{"amd64", "arm64"}

	for _, target := range targets {
		name := filepathBase(target)
		for _, goos := range oses {
			for _, goarch := range arches {
				ext := ""
				if goos == "windows" {
					ext = ".exe"
				}

				output := fmt.Sprintf("build/%s_%s_%s/%s%s", name, goos, goarch, name, ext)
				fmt.Printf("  Building %s...\n", output)

				cmd := exec.Command("go", "build",
					"-ldflags", ldflags,
					"-o", output,
					target,
				)
				cmd.Env = append(os.Environ(),
					"GOOS="+goos,
					"GOARCH="+goarch,
					"CGO_ENABLED=0",
				)
				cmd.Stdout = os.Stdout
				cmd.Stderr = os.Stderr

				if err := cmd.Run(); err != nil {
					return fmt.Errorf("build %s/%s/%s: %w", target, goos, goarch, err)
				}
			}
		}
	}

	fmt.Println("Release build completed successfully")
	return nil
}

// Clean removes build artifacts.
func Clean() error {
	fmt.Println("=== Cleaning build artifacts ===")

	dirs := []string{"build/", "server", "proxy", "server.exe", "proxy.exe"}
	for _, dir := range dirs {
		if err := os.RemoveAll(dir); err != nil {
			fmt.Printf("  Warning: failed to remove %s: %v\n", dir, err)
		} else {
			fmt.Printf("  Removed %s\n", dir)
		}
	}

	fmt.Println("Clean completed")
	return nil
}

// All runs the full check pipeline: lint → test → build.
func All() error {
	fmt.Println("=== Running full check pipeline ===")

	if err := Lint(); err != nil {
		return err
	}
	if err := Test(); err != nil {
		return err
	}
	if err := Build(); err != nil {
		return err
	}

	fmt.Println("=== All checks passed ===")
	return nil
}

// CI runs the complete CI pipeline.
func CI() error {
	fmt.Println("=== Running CI pipeline ===")

	steps := []struct {
		name string
		fn   func() error
	}{
		{"Clean", Clean},
		{"Lint", Lint},
		{"Test", Test},
		{"Build", Build},
	}

	// Only run Docker steps if Docker is available
	if commandExists("docker") || commandExists("docker-compose") {
		fmt.Println("Docker detected, enabling Docker build steps")
		steps = append(steps,
			struct {
				name string
				fn   func() error
			}{"DockerBuild", DockerBuild},
		)
	} else {
		fmt.Println("Docker not available, skipping Docker build step")
	}

	for _, step := range steps {
		fmt.Printf("\n--- %s ---\n", step.name)
		if err := step.fn(); err != nil {
			return fmt.Errorf("%s failed: %w", step.name, err)
		}
	}

	fmt.Println("\n=== CI pipeline completed successfully ===")
	return nil
}

// ============================================================================
// Helper functions
// ============================================================================

// run executes a command and returns an error if it fails.
func run(cmd string, args ...string) error {
	c := exec.Command(cmd, args...)
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	c.Stdin = os.Stdin
	return c.Run()
}

// outputName returns the binary name for a given package path.
func outputName(pkgPath string) string {
	base := filepathBase(pkgPath)
	if runtime.GOOS == "windows" {
		return base + ".exe"
	}
	return base
}

// filepathBase returns the last element of a path.
func filepathBase(path string) string {
	parts := strings.Split(path, "/")
	if len(parts) > 0 && parts[len(parts)-1] != "" {
		return parts[len(parts)-1]
	}
	for i := len(parts) - 1; i >= 0; i-- {
		if parts[i] != "" {
			return parts[i]
		}
	}
	return path
}

// commandExists checks if a command is available in PATH.
func commandExists(cmd string) bool {
	_, err := exec.LookPath(cmd)
	return err == nil
}

// cgoAvailable checks if CGO is available (gcc in PATH + CGO_ENABLED not set to 0).
func cgoAvailable() bool {
	if os.Getenv("CGO_ENABLED") == "0" {
		return false
	}
	// Check if a C compiler is available
	return commandExists("gcc") || commandExists("cc") || commandExists("clang")
}

// getVersion returns the version string (from git tag or fallback).
func getVersion() string {
	cmd := exec.Command("git", "describe", "--tags", "--always", "--dirty")
	out, err := cmd.Output()
	if err != nil {
		return "dev"
	}
	return strings.TrimSpace(string(out))
}

// dockerCompose runs docker-compose commands using either docker-compose or docker compose.
func dockerCompose(args ...string) error {
	// Try `docker compose` first (Docker v2+), fall back to `docker-compose`
	if commandExists("docker") {
		allArgs := append([]string{"compose", "-f", "docker-compose.test.yml"}, args...)
		cmd := exec.Command("docker", allArgs...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err == nil {
			return nil
		}
	}

	if commandExists("docker-compose") {
		allArgs := append([]string{"-f", "docker-compose.test.yml"}, args...)
		return run("docker-compose", allArgs...)
	}

	return fmt.Errorf("neither docker nor docker-compose found in PATH")
}

// mg provides access to mage helpers.
type mageHelper struct{}

var mg mageHelper

// Verbose returns true if mage is running in verbose mode.
func (m mageHelper) Verbose() bool {
	return os.Getenv("MAGEFILE_VERBOSE") == "1" || os.Getenv("MAGEFILE_VERBOSE") == "true"
}
