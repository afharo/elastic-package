// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package archetype

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/elastic/package-spec/v2/code/go/pkg/validator"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"

	"github.com/elastic/elastic-package/internal/packages"
)

func TestPackage(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		pd := createPackageDescriptorForTest()

		err := createAndCheckPackage(t, pd)
		require.NoError(t, err)
	})
	t.Run("missing-version", func(t *testing.T) {
		pd := createPackageDescriptorForTest()
		pd.Manifest.Version = ""

		err := createAndCheckPackage(t, pd)
		require.Error(t, err)
	})
}

func createAndCheckPackage(t require.TestingT, pd PackageDescriptor) error {
	wd, err := os.Getwd()
	require.NoError(t, err)

	tempDir, err := os.MkdirTemp("", "archetype-create-package-")
	require.NoError(t, err)

	os.Chdir(tempDir)
	defer func() {
		os.Chdir(wd)
		os.RemoveAll(tempDir)
	}()

	err = CreatePackage(pd)
	require.NoError(t, err)

	err = checkPackage(pd.Manifest.Name)
	return err
}

func createPackageDescriptorForTest() PackageDescriptor {
	return PackageDescriptor{
		Manifest: packages.PackageManifest{
			Name:    "go_unit_test_package",
			Title:   "Go Unit Test Package",
			Type:    "integration",
			Version: "1.2.3",
			Conditions: packages.Conditions{
				Kibana: packages.KibanaConditions{
					Version: "^7.13.0",
				},
				Elastic: packages.ElasticConditions{
					Subscription: "basic",
				},
			},
			Owner: packages.Owner{
				Github: "mtojek",
			},
			Description: "This package has been generated by a Go unit test.",
			License:     "basic",
			Categories:  []string{"aws", "custom"},
		},
	}
}

func checkPackage(packageName string) error {
	wd, err := os.Getwd()
	if err != nil {
		return errors.Wrap(err, "can't get working directory")
	}
	packageRoot := filepath.Join(wd, packageName)

	os.Chdir(packageRoot)
	defer os.Chdir(wd)

	err = validator.ValidateFromPath(packageRoot)
	if err != nil {
		return errors.Wrap(err, "linting package failed")
	}
	return nil
}
