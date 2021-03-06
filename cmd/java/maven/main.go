// Copyright 2020 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Implements java/maven buildpack.
// The maven buildpack builds Maven applications.
package main

import (
	"fmt"
	"net/http"
	"path/filepath"

	gcp "github.com/GoogleCloudPlatform/buildpacks/pkg/gcpbuildpack"
	"github.com/GoogleCloudPlatform/buildpacks/pkg/java"
	"github.com/buildpack/libbuildpack/layers"
)

const (
	// TODO(b/151198698): Automate Maven version updates.
	mavenVersion = "3.6.3"
	mavenURL     = "https://downloads.apache.org/maven/maven-3/%[1]s/binaries/apache-maven-%[1]s-bin.tar.gz"
	mavenLayer   = "maven"
	m2Layer      = "m2"
)

// mavenMetadata represents metadata stored for a maven layer.
type mavenMetadata struct {
	Version string `toml:"version"`
}

func main() {
	gcp.Main(detectFn, buildFn)
}

func detectFn(ctx *gcp.Context) error {
	if !ctx.FileExists("pom.xml") {
		ctx.OptOut("pom.xml not found.")
	}
	return nil
}

func buildFn(ctx *gcp.Context) error {
	var repoMeta java.RepoMetadata
	m2CachedRepo := ctx.Layer(m2Layer)
	ctx.ReadMetadata(m2CachedRepo, &repoMeta)
	java.CheckCacheExpiration(ctx, &repoMeta, m2CachedRepo)

	var mvn string
	var err error
	if ctx.FileExists("mvnw") {
		mvn = "./mvnw"
	} else if mvnInstalled(ctx) {
		mvn = "mvn"
	} else {
		mvn, err = installMaven(ctx)
		if err != nil {
			return fmt.Errorf("installing Maven: %w", err)
		}
	}

	command := []string{mvn, "clean", "package", "--batch-mode", "-DskipTests", "-Dmaven.repo.local=" + m2CachedRepo.Root}
	if !ctx.Debug() {
		command = append(command, "--quiet")
	}
	ctx.ExecUser(command)
	ctx.WriteMetadata(m2CachedRepo, &repoMeta, layers.Cache)

	return nil
}

func mvnInstalled(ctx *gcp.Context) bool {
	result := ctx.Exec([]string{"bash", "-c", "command -v mvn || true"})
	return result.Stdout != ""
}

// installMaven installs Maven and returns the path of the mvn binary
func installMaven(ctx *gcp.Context) (string, error) {
	mvnl := ctx.Layer(mavenLayer)

	// Check the metadata in the cache layer to determine if we need to proceed.
	var meta mavenMetadata
	ctx.ReadMetadata(mvnl, &meta)

	if mavenVersion == meta.Version {
		ctx.CacheHit(mavenLayer)
		ctx.Logf("Maven cache hit, skipping installation.")
		return filepath.Join(mvnl.Root, "bin", "mvn"), nil
	}
	ctx.CacheMiss(mavenLayer)
	ctx.ClearLayer(mvnl)

	// Download and install maven in layer.
	ctx.Logf("Installing Maven v%s", mavenVersion)
	archiveURL := fmt.Sprintf(mavenURL, mavenVersion)
	if code := ctx.HTTPStatus(archiveURL); code != http.StatusOK {
		return "", gcp.UserErrorf("Maven version %s does not exist at %s (status %d).", mavenVersion, archiveURL, code)
	}
	command := fmt.Sprintf("curl --fail --show-error --silent --location --retry 3 %s | tar xz --directory %s --strip-components=1", archiveURL, mvnl.Root)
	ctx.Exec([]string{"bash", "-c", command})

	meta.Version = mavenVersion
	ctx.WriteMetadata(mvnl, meta, layers.Cache)
	return filepath.Join(mvnl.Root, "bin", "mvn"), nil
}
