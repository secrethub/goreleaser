// Package alpine provides a Pipe that generates .apk and APKINDEX files for an Alpine repositories
package alpine

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"text/template"

	"github.com/apex/log"

	"github.com/goreleaser/goreleaser/internal/artifact"
	"github.com/goreleaser/goreleaser/internal/pipe"
	"github.com/goreleaser/goreleaser/pkg/config"
	"github.com/goreleaser/goreleaser/pkg/context"
)

const (
	apkBuildFileName = "APKBUILD"
	apkIndexFileName = "APKINDEX.tar.gz"
	abuildOutputDir  = "dist"
)

var (
	pubKeyPath  = os.Getenv("PACKAGER_PUBKEY")
	privKeyPath = os.Getenv("PACKAGER_PRIVKEY")

	// ErrNoAlpineKeys is shown when the required environement variables are not set
	ErrNoAlpineKeys = errors.New("environment variables PACKAGER_PUBKEY and PACKAGER_PRIVKEY need to be set")
)

// Pipe for Alpine publishing
type Pipe struct{}

// String returns the description of the pipe
func (Pipe) String() string {
	return "Alpine"
}

// Default sets the pipe defaults
func (Pipe) Default(ctx *context.Context) error {
	for i := range ctx.Config.Alpine {
		alpine := &ctx.Config.Alpine[i]

		if alpine.Root == "" {
			alpine.Root = "alpine"
		}

		if alpine.Branch == "" {
			alpine.Branch = "edge"
		}

		if alpine.Repository == "" {
			alpine.Repository = "main"
		}

		if pubKeyPath == "" || privKeyPath == "" {
			return ErrNoAlpineKeys
		}
	}
	return nil
}

// Run the pipe
func (Pipe) Run(ctx *context.Context) error {
	if len(ctx.Config.Alpine) == 0 {
		return pipe.Skip("alpine section is not configured")
	}

	// Handle every configured Alpine
	for _, alpine := range ctx.Config.Alpine {
		artifacts := ctx.Artifacts.Filter(
			artifact.And(
				artifact.ByGoos("linux"),
				artifact.ByGoarm(""),
				artifact.ByType(artifact.Binary),
			),
		).List()

		log.Debugf("will build %d artifacts", len(artifacts))

		pwd, err := os.Getwd()
		if err != nil {
			return err
		}

		localPath := filepath.Join(ctx.Config.Dist, "alpine-"+alpine.Name)
		localPathAbs := filepath.Join(pwd, localPath)

		err = os.MkdirAll(localPath, 0700)
		if err != nil {
			return err
		}

		apkBuild, err := generateApkBuildFile(ctx, alpine)
		if err != nil {
			return err
		}

		log.WithField(apkBuildFileName, localPath).Info("writing")
		err = ioutil.WriteFile(filepath.Join(localPath, apkBuildFileName), apkBuild, 0644)

		if _, err = exec.LookPath("abuild"); err != nil {
			return err
		}

		repoPath := filepath.Join(alpine.Root, alpine.Branch, alpine.Repository)
		pubKeyName := filepath.Dir(pubKeyPath)

		for _, binArtifact := range artifacts {
			arch := binArtifact.Goarch
			switch arch {
			case "386":
				arch = "x86"
			case "amd64":
				arch = "x86_64"
			}

			artifactRepoPath := filepath.Join(repoPath, arch)

			binDir := filepath.Join(localPath, arch)
			err := os.Mkdir(binDir, 0700)
			if err != nil {
				return err
			}

			binary, err := os.Open(binArtifact.Path)
			if err != nil {
				return err
			}

			destPath := filepath.Join(binDir, filepath.Base(binArtifact.Path))
			destination, err := os.Create(destPath)
			if err != nil {
				return err
			}
			os.Chmod(destPath, 0555)

			_, err = io.Copy(destination, binary)
			if err != nil {
				return err
			}

			cmd := exec.Command("abuild", "-P", localPathAbs, "-r")
			cmd.Env = append(os.Environ(), "CBUILD="+arch)
			cmd.Dir = localPath
			output, err := cmd.CombinedOutput()
			if err != nil {
				return fmt.Errorf("%s:\n%s", err, string(output))
			}

			artifactMeta := map[string]interface{}{
				"AlpineArch": arch,
			}

			abuildOutputPath := filepath.Join(localPath, abuildOutputDir, arch)

			apkFileName := fmt.Sprintf("%s-%s-r%d.apk", alpine.Name, ctx.Version, alpine.Rel)
			apkFilePath := filepath.Join(abuildOutputPath, apkFileName)

			ctx.Artifacts.Add(artifact.Artifact{
				Type:    artifact.APK,
				Name:    apkFileName,
				Path:    apkFilePath,
				Goos:    "linux",
				Goarch:  binArtifact.Goarch,
				Goarm:   "",
				RepoDir: artifactRepoPath,
				Extra:   artifactMeta,
			})

			apkIndexPath := filepath.Join(abuildOutputPath, apkIndexFileName)
			apkIndexPathAbs := filepath.Join(pwd, apkIndexPath)

			cmd = exec.Command("abuild-sign", apkIndexPathAbs, "-p", pubKeyName)
			cmd.Dir = localPath
			output, err = cmd.CombinedOutput()
			if err != nil {
				return fmt.Errorf("%s:\n%s", err, string(output))
			}

			ctx.Artifacts.Add(artifact.Artifact{
				Type:    artifact.APKIndex,
				Name:    apkIndexFileName,
				Path:    apkIndexPath,
				Goos:    "linux",
				Goarch:  binArtifact.Goarch,
				Goarm:   "",
				RepoDir: artifactRepoPath,
				Extra:   artifactMeta,
			})
		}
	}

	return nil
}

func generateApkBuildFile(ctx *context.Context, info config.Alpine) ([]byte, error) {
	binaries := make(map[string]string)
	for _, build := range ctx.Config.Builds {
		binary := filepath.Base(build.Binary)
		binaries[binary] = filepath.Join("$pkgarch", binary)
	}

	data := apkBuildData{
		Info:     info,
		Version:  ctx.Version,
		Binaries: binaries,
	}

	return buildApkBuildFile(data)
}

const apkBuildTemplate = `
{{- if .Info.Contributor -}}
# Contributor: {{ .Info.Contributor }}
{{ end -}}
{{- if .Info.Maintainer -}}
# Maintainer: {{ .Info.Maintainer }}
{{ end -}}
pkgname={{ .Info.Name }}
pkgver={{ .Version }}
pkgrel={{ .Info.Rel }}
pkgdesc="{{ .Info.Description }}"
url="{{ .Info.URL }}"
arch="all"
license="{{ .Info.License }}"
depends=""
makedepends=""
install=""
subpackages=""
source=""
builddir=""
{{- if not .Info.CheckFn }}
options="!check"
{{ end }}

{{- if .Info.CheckFn }}
check() {
	cd "$builddir"
	{{ .Info.CheckFn }}
}

{{ end }}
package() {
	{{ range $key, $value := .Binaries -}}
	install -Dm755 "{{ $value }}" "$pkgdir/usr/bin/{{ $key }}"
	{{ end }}
}
`

type apkBuildData struct {
	Info        config.Alpine
	Binaries    map[string]string
	Version     string
	ProjectDir  string
	MakeDepends string
}

func buildApkBuildFile(data apkBuildData) (out []byte, err error) {
	w := &bytes.Buffer{}
	t, err := template.New(data.Info.Name).Parse(apkBuildTemplate)
	if err != nil {
		return out, err
	}
	err = t.Execute(w, data)

	return w.Bytes(), err
}
