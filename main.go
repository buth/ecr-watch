package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ecr"
	"github.com/kelseyhightower/envconfig"
)

var config struct {
	AWSProfile string        `envconfig:"AWS_PROFILE"`
	AWSRegion  string        `envconfig:"AWS_REGION" default:"us-east-1"`
	Repository string        `required:"true"`
	TagPattern string        `envconfig:"TAG_PATTERN" default:"^latest$" required:"true"`
	Interval   time.Duration `required:"true" default:"30s" required:"true"`
}

func main() {
	help := flag.Bool("h", false, "print usage")
	flag.Parse()

	if help != nil && *help {
		envconfig.Usage("ecr_watch", &config)
		return
	}

	logger := log.New(os.Stderr, "", log.LstdFlags)
	if err := envconfig.Process("ecr_watch", &config); err != nil {
		logger.Fatal(err)
	}

	tagRegexp, err := regexp.Compile(config.TagPattern)
	if err != nil {
		logger.Fatal(err)
	}

	awsSessionOptions := session.Options{
		Profile: config.AWSProfile,
		Config: aws.Config{
			Region: aws.String(config.AWSRegion),
			CredentialsChainVerboseErrors: aws.Bool(true),
		},
	}

	awsSession, err := session.NewSessionWithOptions(awsSessionOptions)
	if err != nil {
		logger.Fatal(err)
	}

	logger.Println("running")
	ecrClient := ecr.New(awsSession)
	mostRecentImagePushedAt := time.Time{}
	for {
		imageIDs := []*ecr.ImageIdentifier{}
		listImagesInput := &ecr.ListImagesInput{
			RepositoryName: aws.String(config.Repository),
		}

		for {
			listImagesOutput, err := ecrClient.ListImages(listImagesInput)
			if err != nil {
				logger.Fatal(err)
			}

			for _, imageID := range listImagesOutput.ImageIds {
				if tag := aws.StringValue(imageID.ImageTag); tagRegexp.MatchString(tag) {
					logger.Printf("matched tag: %s", tag)
					imageIDs = append(imageIDs, imageID)
				}
			}

			if nextToken := listImagesOutput.NextToken; nextToken != nil {
				listImagesInput.NextToken = nextToken
			} else {
				break
			}
		}

		describeImagesInput := &ecr.DescribeImagesInput{
			RepositoryName: aws.String(config.Repository),
			ImageIds:       imageIDs,
		}

		describeImagesOutput, err := ecrClient.DescribeImages(describeImagesInput)
		if err != nil {
			logger.Fatal(err)
		}

		currentMostRecentImagePushedAt := time.Time{}
		currentMostRecentImageTags := []string{}
		for _, imageDetail := range describeImagesOutput.ImageDetails {
			if imagePushedAt := aws.TimeValue(imageDetail.ImagePushedAt); imagePushedAt.After(currentMostRecentImagePushedAt) {
				currentMostRecentImagePushedAt = imagePushedAt
				currentMostRecentImageTags = aws.StringValueSlice(imageDetail.ImageTags)
			}
		}

		if !mostRecentImagePushedAt.IsZero() && currentMostRecentImagePushedAt.After(mostRecentImagePushedAt) {
			logger.Println("exiting")
			fmt.Print(strings.Join(currentMostRecentImageTags, ","))
			return
		}

		mostRecentImagePushedAt = currentMostRecentImagePushedAt
		logger.Printf("most recent image pushed at %s", mostRecentImagePushedAt)
		logger.Printf("sleeping for %s", config.Interval)
		time.Sleep(config.Interval)
	}
}
