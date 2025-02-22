package mirrosa

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"go.uber.org/zap"
)

const dhcpOptionsDescription = "DHCP Option Sets configure how devices uses the DHCP protocol within a VPC [1]. " +
	"By default the 'Domain Name Servers' used is AmazonProvidedDNS and the 'Domain name' is ec2.internal in us-east-1 " +
	"and ${region}.compute.internal in other regions. This cannot be modified for non-BYOVPC ROSA clusters.\n\n" +
	"With BYOVPC ROSA clusters, the DHCP Option Set can be modified, but crucially its 'Domain name' must not contain " +
	"uppercase letters (AWS allows uppercase letters, but Kubernetes DNS does not) [2] nor spaces [3]." +
	"\n\nReferences:\n" +
	"1. https://docs.aws.amazon.com/vpc/latest/userguide/VPC_DHCP_Options.html\n" +
	"2. https://kubernetes.io/docs/concepts/overview/working-with-objects/names/#dns-subdomain-names" +
	"3. https://github.com/coreos/bugs/issues/1934"

// Ensure DhcpOptions implements Component
var _ Component = &DhcpOptions{}

type MirrosaDhcpOptionsAPIClient interface {
	ec2.DescribeDhcpOptionsAPIClient
	ec2.DescribeVpcsAPIClient
}

type DhcpOptions struct {
	log   *zap.SugaredLogger
	VpcId string

	Ec2Client MirrosaDhcpOptionsAPIClient
}

func (c *Client) NewDhcpOptions() DhcpOptions {
	return DhcpOptions{
		log:       c.log,
		VpcId:     c.ClusterInfo.VpcId,
		Ec2Client: ec2.NewFromConfig(c.AwsConfig),
	}
}

func (d DhcpOptions) Validate(ctx context.Context) error {
	d.log.Debugf("validating that the attached DHCP Options Set has no uppercase characters in its domain name(s)")
	vpcResp, err := d.Ec2Client.DescribeVpcs(ctx, &ec2.DescribeVpcsInput{
		VpcIds: []string{d.VpcId},
	})
	if err != nil {
		return err
	}

	if len(vpcResp.Vpcs) != 1 {
		return fmt.Errorf("unexpectedly received %d VPCs when describing: %s", len(vpcResp.Vpcs), d.VpcId)
	}
	dhcpOptionsId := *vpcResp.Vpcs[0].DhcpOptionsId

	dhcpResp, err := d.Ec2Client.DescribeDhcpOptions(ctx, &ec2.DescribeDhcpOptionsInput{
		DhcpOptionsIds: []string{dhcpOptionsId},
	})
	if err != nil {
		return err
	}

	if len(dhcpResp.DhcpOptions) != 1 {
		return fmt.Errorf("unexepctedly received %d DHCP Options Sets when describing: %s", len(dhcpResp.DhcpOptions), dhcpOptionsId)
	}

	for _, config := range dhcpResp.DhcpOptions[0].DhcpConfigurations {
		switch *config.Key {
		case "domain-name":
			for _, v := range config.Values {
				d.log.Debugf("validating DHCP Options Set domain name: %s", *v.Value)
				if *v.Value != strings.ToLower(*v.Value) {
					return fmt.Errorf("DHCP Options set: %s contains uppercase letters in the domain name: %s", dhcpOptionsId, *v.Value)
				} else if strings.Contains(*v.Value, " ") {
					return fmt.Errorf("DHCP Options set: %s contains a space in the domain name: %s", dhcpOptionsId, *v.Value)
				}
			}
		default:
			// Other DHCP Options set configurations have no hard rules
			continue
		}
	}

	return nil
}

func (d DhcpOptions) Description() string {
	return dhcpOptionsDescription
}

func (d DhcpOptions) FilterValue() string {
	return d.Title()
}

func (d DhcpOptions) Title() string {
	return "DHCP Option Set"
}
