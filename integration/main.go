package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"time"

	ocmlog "github.com/openshift-online/ocm-sdk-go/logging"
	inttestaws "github.com/openshift/osd-network-verifier/integration/pkg/aws"
	"github.com/openshift/osd-network-verifier/pkg/verifier"
	awsverifier "github.com/openshift/osd-network-verifier/pkg/verifier/aws"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
)

func main() {
	region := flag.String("region", "us-east-1", "AWS Region")
	profile := flag.String("profile", "", "AWS Profile")
	createOnly := flag.Bool("create-only", false, "When specified, only create infrastructure and do not delete")
	deleteOnly := flag.Bool("delete-only", false, "When specified, delete infrastructure in an idempotent fashion")
	flag.Parse()

	var (
		cfg aws.Config
		err error
	)

	if *profile == "" {
		cfg, err = config.LoadDefaultConfig(context.TODO(), config.WithRegion(*region))
		if err != nil {
			panic(err)
		}
	} else {
		cfg, err = config.LoadDefaultConfig(context.TODO(), config.WithRegion(*region), config.WithSharedConfigProfile(*profile))
		if err != nil {
			panic(err)
		}
	}

	data := inttestaws.NewIntegrationTestData(cfg)
	if *deleteOnly {
		if err := data.Cleanup(context.TODO()); err != nil {
			panic(err)
		}

		return
	}

	if err := data.Setup(context.TODO()); err != nil {
		log.Printf("setup err, starting cleanup: %s", err)
		if err := data.Cleanup(context.TODO()); err != nil {
			panic(err)
		}
	}

	if *createOnly {
		// Don't run egress check and cleanup afterwards
		return
	}

	if err := onvEgressCheck(cfg, *data.GetPrivateSubnetId()); err != nil {
		panic(err)
	}

	if err := data.Cleanup(context.TODO()); err != nil {
		panic(err)
	}
}

func onvEgressCheck(cfg aws.Config, subnetId string) error {
	builder := ocmlog.NewStdLoggerBuilder()
	logger, err := builder.Build()
	if err != nil {
		return fmt.Errorf("unable to build logger: %s", err)
	}

	awsVerifier, err := awsverifier.NewAwsVerifierFromConfig(cfg, logger)
	if err != nil {
		return err
	}

	// Example required values
	defaultTags := map[string]string{"osd-network-verifier": "owned", "red-hat-managed": "true", "Name": "osd-network-verifier"}

	vei := verifier.ValidateEgressInput{
		Timeout:      2 * time.Second,
		Ctx:          context.TODO(),
		SubnetID:     subnetId,
		InstanceType: "t3.micro",
		Tags:         defaultTags,
	}

	// Call egress validator
	log.Println("Starting ONV egress validation")
	out := verifier.ValidateEgress(awsVerifier, vei)
	out.Summary(false)
	egressFailures := out.GetEgressURLFailures()
	for _, ef := range egressFailures {
		log.Printf("egress failure: %s", ef.EgressURL())
	}

	if out.IsSuccessful() {
		log.Println("ONV egress validation: Success!")
	} else {
		log.Println("ONV egress validation: Failure!")
	}

	return nil
}
