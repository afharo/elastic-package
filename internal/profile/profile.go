// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package profile

import (
	"embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/elastic/go-resource"

	"github.com/elastic/elastic-package/internal/configuration/locations"
	"github.com/elastic/elastic-package/internal/files"
)

const (
	// PackageProfileMetaFile is the filename of the profile metadata file
	PackageProfileMetaFile = "profile.json"

	// PackageProfileConfigFile is the filename of the profile configuration file
	PackageProfileConfigFile = "config.yml"

	// DefaultProfile is the name of the default profile.
	DefaultProfile = "default"
)

//go:embed _static
var static embed.FS

var (
	staticSource     = resource.NewSourceFS(static)
	profileResources = []resource.Resource{
		&resource.File{
			Path:    PackageProfileMetaFile,
			Content: profileMetadataContent,
		},
		&resource.File{
			Path:    PackageProfileConfigFile + ".example",
			Content: staticSource.File("_static/config.yml.example"),
		},
	}
)

type Options struct {
	PackagePath       string
	Name              string
	FromProfile       string
	OverwriteExisting bool
}

func CreateProfile(options Options) error {
	if options.PackagePath == "" {
		loc, err := locations.NewLocationManager()
		if err != nil {
			return fmt.Errorf("error finding profile dir location: %w", err)
		}
		options.PackagePath = loc.ProfileDir()
	}

	if options.Name == "" {
		options.Name = DefaultProfile
	}

	if !options.OverwriteExisting {
		_, err := loadProfile(options.PackagePath, options.Name)
		if err == nil {
			return fmt.Errorf("profile %q already exists", options.Name)
		}
		if err != nil && err != ErrNotAProfile {
			return fmt.Errorf("failed to check if profile %q exists: %w", options.Name, err)
		}
	}

	// If they're creating from Default, assume they want the actual default, and
	// not whatever is currently inside default.
	if from := options.FromProfile; from != "" && from != DefaultProfile {
		return createProfileFrom(options)
	}

	return createProfile(options, profileResources)
}

func createProfile(options Options, resources []resource.Resource) error {
	profileDir := filepath.Join(options.PackagePath, options.Name)

	resourceManager := resource.NewManager()
	resourceManager.AddFacter(resource.StaticFacter{
		"profile_name": options.Name,
		"profile_path": profileDir,
	})

	os.MkdirAll(profileDir, 0755)
	resourceManager.RegisterProvider("file", &resource.FileProvider{
		Prefix: profileDir,
	})

	results, err := resourceManager.Apply(resources)
	if err != nil {
		var errors []string
		for _, result := range results {
			if err := result.Err(); err != nil {
				errors = append(errors, err.Error())
			}
		}
		return fmt.Errorf("%w: %s", err, strings.Join(errors, ", "))
	}

	return nil
}

func createProfileFrom(options Options) error {
	from, err := LoadProfile(options.FromProfile)
	if err != nil {
		return fmt.Errorf("failed to load profile to copy %q: %w", options.FromProfile, err)
	}

	profileDir := filepath.Join(options.PackagePath, options.Name)
	err = files.CopyAll(from.ProfilePath, profileDir)
	if err != nil {
		return fmt.Errorf("failed to copy files from profile %q to %q", options.FromProfile, options.Name)
	}

	overwriteOptions := options
	overwriteOptions.OverwriteExisting = true
	return createProfile(overwriteOptions, profileResources)
}

// Profile manages a a given user config profile
type Profile struct {
	// ProfilePath is the absolute path to the profile
	ProfilePath string
	ProfileName string

	config config
}

// Path returns an absolute path to the given file
func (profile Profile) Path(names ...string) string {
	elems := append([]string{profile.ProfilePath}, names...)
	return filepath.Join(elems...)
}

// Config returns a configuration setting, or its default if setting not found
func (profile Profile) Config(name string, def string) string {
	v, found := profile.config.get(name)
	if !found {
		return def
	}
	return v
}

// ErrNotAProfile is returned in cases where we don't have a valid profile directory
var ErrNotAProfile = errors.New("not a profile")

// ComposeEnvVars returns a list of environment variables that can be passed
// to docker-compose for the sake of filling out paths and names in the snapshot.yml file.
func (profile Profile) ComposeEnvVars() []string {
	return []string{
		fmt.Sprintf("PROFILE_NAME=%s", profile.ProfileName),
	}
}

// DeleteProfile deletes a profile from the default elastic-package config dir
func DeleteProfile(profileName string) error {
	if profileName == DefaultProfile {
		return errors.New("cannot remove default profile")
	}

	loc, err := locations.NewLocationManager()
	if err != nil {
		return fmt.Errorf("error finding stack dir location: %w", err)
	}

	pathToDelete := filepath.Join(loc.ProfileDir(), profileName)
	return os.RemoveAll(pathToDelete)
}

// FetchAllProfiles returns a list of profile values
func FetchAllProfiles(elasticPackagePath string) ([]Metadata, error) {
	dirList, err := os.ReadDir(elasticPackagePath)
	if errors.Is(err, os.ErrNotExist) {
		return []Metadata{}, nil
	}
	if err != nil {
		return []Metadata{}, fmt.Errorf("error reading from directory %s: %w", elasticPackagePath, err)
	}

	var profiles []Metadata
	// TODO: this should read a profile.json file or something like that
	for _, item := range dirList {
		if !item.IsDir() {
			continue
		}
		profile, err := loadProfile(elasticPackagePath, item.Name())
		if errors.Is(err, ErrNotAProfile) {
			continue
		}
		if err != nil {
			return profiles, fmt.Errorf("error loading profile %s: %w", item.Name(), err)
		}
		metadata, err := loadProfileMetadata(filepath.Join(profile.ProfilePath, PackageProfileMetaFile))
		if err != nil {
			return profiles, fmt.Errorf("error reading profile metadata: %w", err)
		}
		profiles = append(profiles, metadata)
	}
	return profiles, nil
}

// LoadProfile loads an existing profile from the default elastic-package config dir.
func LoadProfile(profileName string) (*Profile, error) {
	loc, err := locations.NewLocationManager()
	if err != nil {
		return nil, fmt.Errorf("error finding stack dir location: %w", err)
	}

	return loadProfile(loc.ProfileDir(), profileName)
}

// loadProfile loads an existing profile
func loadProfile(elasticPackagePath string, profileName string) (*Profile, error) {
	profilePath := filepath.Join(elasticPackagePath, profileName)

	isValid, err := isProfileDir(profilePath)
	if err != nil {
		return nil, fmt.Errorf("error checking profile %q: %w", profileName, err)
	}
	if !isValid {
		return nil, ErrNotAProfile
	}

	configPath := filepath.Join(profilePath, PackageProfileConfigFile)
	config, err := loadProfileConfig(configPath)
	if err != nil {
		return nil, fmt.Errorf("error loading configuration for profile %q: %w", profileName, err)
	}

	profile := Profile{
		ProfileName: profileName,
		ProfilePath: profilePath,
		config:      config,
	}

	return &profile, nil
}

// isProfileDir checks to see if the given path points to a valid profile
func isProfileDir(path string) (bool, error) {
	metaPath := filepath.Join(path, string(PackageProfileMetaFile))
	_, err := os.Stat(metaPath)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("error stat: %s: %w", metaPath, err)
	}
	return true, nil
}
