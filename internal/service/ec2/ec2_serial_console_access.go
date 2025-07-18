// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package ec2

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-provider-aws/internal/conns"
	"github.com/hashicorp/terraform-provider-aws/internal/errs/sdkdiag"
	"github.com/hashicorp/terraform-provider-aws/names"
)

// @SDKResource("aws_ec2_serial_console_access", name="Serial Console Access")
// @Region(global=true)
// @SingletonIdentity
// @Testing(hasExistsFunction=false)
// @Testing(generator=false)
func resourceSerialConsoleAccess() *schema.Resource {
	return &schema.Resource{
		CreateWithoutTimeout: resourceSerialConsoleAccessCreate,
		ReadWithoutTimeout:   resourceSerialConsoleAccessRead,
		UpdateWithoutTimeout: resourceSerialConsoleAccessUpdate,
		DeleteWithoutTimeout: resourceSerialConsoleAccessDelete,

		Schema: map[string]*schema.Schema{
			names.AttrEnabled: {
				Type:     schema.TypeBool,
				Optional: true,
				Default:  true,
			},
		},
	}
}

func resourceSerialConsoleAccessCreate(ctx context.Context, d *schema.ResourceData, meta any) diag.Diagnostics {
	var diags diag.Diagnostics

	conn := meta.(*conns.AWSClient).EC2Client(ctx)

	enabled := d.Get(names.AttrEnabled).(bool)
	if err := setSerialConsoleAccess(ctx, conn, enabled); err != nil {
		return sdkdiag.AppendErrorf(diags, "setting EC2 Serial Console Access (%t): %s", enabled, err)
	}

	d.SetId(meta.(*conns.AWSClient).AccountID(ctx))

	return append(diags, resourceSerialConsoleAccessRead(ctx, d, meta)...)
}

func resourceSerialConsoleAccessRead(ctx context.Context, d *schema.ResourceData, meta any) diag.Diagnostics {
	var diags diag.Diagnostics

	conn := meta.(*conns.AWSClient).EC2Client(ctx)

	input := ec2.GetSerialConsoleAccessStatusInput{}
	output, err := conn.GetSerialConsoleAccessStatus(ctx, &input)

	if err != nil {
		return sdkdiag.AppendErrorf(diags, "reading EC2 Serial Console Access: %s", err)
	}

	d.Set(names.AttrEnabled, output.SerialConsoleAccessEnabled)

	return diags
}

func resourceSerialConsoleAccessUpdate(ctx context.Context, d *schema.ResourceData, meta any) diag.Diagnostics {
	var diags diag.Diagnostics

	conn := meta.(*conns.AWSClient).EC2Client(ctx)

	enabled := d.Get(names.AttrEnabled).(bool)
	if err := setSerialConsoleAccess(ctx, conn, enabled); err != nil {
		return sdkdiag.AppendErrorf(diags, "updating EC2 Serial Console Access (%t): %s", enabled, err)
	}

	return append(diags, resourceSerialConsoleAccessRead(ctx, d, meta)...)
}

func resourceSerialConsoleAccessDelete(ctx context.Context, d *schema.ResourceData, meta any) diag.Diagnostics {
	var diags diag.Diagnostics

	conn := meta.(*conns.AWSClient).EC2Client(ctx)

	// Removing the resource disables serial console access.
	if err := setSerialConsoleAccess(ctx, conn, false); err != nil {
		return sdkdiag.AppendErrorf(diags, "disabling EC2 Serial Console Access: %s", err)
	}

	return diags
}

func setSerialConsoleAccess(ctx context.Context, conn *ec2.Client, enabled bool) error {
	var err error

	if enabled {
		input := ec2.EnableSerialConsoleAccessInput{}
		_, err = conn.EnableSerialConsoleAccess(ctx, &input)
	} else {
		input := ec2.DisableSerialConsoleAccessInput{}
		_, err = conn.DisableSerialConsoleAccess(ctx, &input)
	}

	return err
}
