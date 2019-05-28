// Package s3 provides a Pipe that push artifacts to s3/minio
package s3

import (
	"os"
	"path/filepath"

	"github.com/apex/log"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/goreleaser/goreleaser/internal/artifact"
	"github.com/goreleaser/goreleaser/internal/pipe"
	"github.com/goreleaser/goreleaser/internal/semerrgroup"
	"github.com/goreleaser/goreleaser/internal/tmpl"
	"github.com/goreleaser/goreleaser/pkg/config"
	"github.com/goreleaser/goreleaser/pkg/context"
	"github.com/pkg/errors"
)

// Pipe for Artifactory
type Pipe struct{}

// String returns the description of the pipe
func (Pipe) String() string {
	return "S3"
}

var (
	artifactTypes = map[string]artifact.Type{
		"archive":   artifact.UploadableArchive,
		"binary":    artifact.UploadableBinary,
		"nfpm":      artifact.LinuxPackage,
		"checksum":  artifact.Checksum,
		"signature": artifact.Signature,
		"apk":       artifact.APK,
		"apkindex":  artifact.APKIndex,
	}
)

// Default sets the pipe defaults
func (Pipe) Default(ctx *context.Context) error {
	for i := range ctx.Config.S3 {
		s3 := &ctx.Config.S3[i]
		if s3.Bucket == "" {
			continue
		}
		if s3.Region == "" {
			s3.Region = "us-east-1"
		}
		if s3.ACL == "" {
			s3.ACL = "private"
		}
	}
	return nil
}

// Publish to S3
func (Pipe) Publish(ctx *context.Context) error {
	if len(ctx.Config.S3) == 0 {
		return pipe.Skip("s3 section is not configured")
	}
	if err := ctx.CheckPipe("s3"); err != nil {
		return err
	}
	var g = semerrgroup.New(ctx.Parallelism)
	for _, conf := range ctx.Config.S3 {
		conf := conf
		g.Go(func() error {
			return upload(ctx, conf)
		})
	}
	return g.Wait()
}

func newS3Svc(conf config.S3) *s3.S3 {
	builder := newSessionBuilder()
	builder.Profile(conf.Profile)
	if conf.Endpoint != "" {
		builder.Endpoint(conf.Endpoint)
		builder.S3ForcePathStyle(true)
	}
	sess := builder.Build()

	return s3.New(sess, &aws.Config{
		Region: aws.String(conf.Region),
	})
}

func upload(ctx *context.Context, conf config.S3) error {
	var svc = newS3Svc(conf)

	template := tmpl.New(ctx)
	bucket, err := template.Apply(conf.Bucket)
	if err != nil {
		return err
	}

	folder, err := template.Apply(conf.Folder)
	if err != nil {
		return err
	}

	var filters []artifact.Filter
	for _, artTypeStr := range conf.Artifacts {
		if artType, ok := artifactTypes[artTypeStr]; ok {
			filters = append(filters, artifact.ByType(artType))
		} else {
			return errors.Errorf("unknown artifact type: %s", artTypeStr)
		}
	}

	var g = semerrgroup.New(ctx.Parallelism)
	for _, artifact := range ctx.Artifacts.Filter(artifact.Or(filters...)).List() {
		artifact := artifact
		g.Go(func() error {
			f, err := os.Open(artifact.Path)
			if err != nil {
				return err
			}
			path := filepath.Join(folder, artifact.RepoDir, artifact.Name)
			log.WithFields(log.Fields{
				"bucket":   bucket,
				"folder":   path,
				"artifact": artifact.Name,
			}).Info("uploading")
			_, err = svc.PutObjectWithContext(ctx, &s3.PutObjectInput{
				Bucket: aws.String(bucket),
				Key:    aws.String(path),
				Body:   f,
				ACL:    aws.String(conf.ACL),
			})
			return err
		})
	}
	return g.Wait()
}
