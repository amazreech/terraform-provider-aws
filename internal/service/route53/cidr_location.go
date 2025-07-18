// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package route53

import (
	"context"
	"fmt"

	"github.com/YakDriver/regexache"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/route53"
	awstypes "github.com/aws/aws-sdk-go-v2/service/route53/types"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/retry"
	"github.com/hashicorp/terraform-provider-aws/internal/errs"
	"github.com/hashicorp/terraform-provider-aws/internal/errs/fwdiag"
	"github.com/hashicorp/terraform-provider-aws/internal/flex"
	"github.com/hashicorp/terraform-provider-aws/internal/framework"
	fwflex "github.com/hashicorp/terraform-provider-aws/internal/framework/flex"
	fwtypes "github.com/hashicorp/terraform-provider-aws/internal/framework/types"
	tfslices "github.com/hashicorp/terraform-provider-aws/internal/slices"
	"github.com/hashicorp/terraform-provider-aws/internal/tfresource"
	"github.com/hashicorp/terraform-provider-aws/names"
)

// @FrameworkResource("aws_route53_cidr_location", name="CIDR Location")
func newCIDRLocationResource(context.Context) (resource.ResourceWithConfigure, error) {
	r := &cidrLocationResource{}

	return r, nil
}

type cidrLocationResource struct {
	framework.ResourceWithModel[cidrLocationResourceModel]
	framework.WithImportByID
}

func (r *cidrLocationResource) Schema(ctx context.Context, request resource.SchemaRequest, response *resource.SchemaResponse) {
	response.Schema = schema.Schema{
		Attributes: map[string]schema.Attribute{
			"cidr_blocks": schema.SetAttribute{
				CustomType:  fwtypes.NewSetTypeOf[fwtypes.CIDRBlock](ctx),
				Required:    true,
				ElementType: fwtypes.CIDRBlockType,
			},
			"cidr_collection_id": schema.StringAttribute{
				Required: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			names.AttrID: framework.IDAttributeDeprecatedNoReplacement(),
			names.AttrName: schema.StringAttribute{
				Required: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
				Validators: []validator.String{
					stringvalidator.LengthAtMost(16),
					stringvalidator.RegexMatches(regexache.MustCompile(`^[0-9A-Za-z_-]+$`), `can include letters, digits, underscore (_) and the dash (-) character`),
				},
			},
		},
	}
}

func (r *cidrLocationResource) Create(ctx context.Context, request resource.CreateRequest, response *resource.CreateResponse) {
	var data cidrLocationResourceModel
	response.Diagnostics.Append(request.Plan.Get(ctx, &data)...)
	if response.Diagnostics.HasError() {
		return
	}

	conn := r.Meta().Route53Client(ctx)

	collectionID := data.CIDRCollectionID.ValueString()
	collection, err := findCIDRCollectionByID(ctx, conn, collectionID)

	if err != nil {
		response.Diagnostics.AddError(fmt.Sprintf("reading Route 53 CIDR Collection (%s)", collectionID), err.Error())

		return
	}

	name := data.Name.ValueString()
	input := &route53.ChangeCidrCollectionInput{
		Changes: []awstypes.CidrCollectionChange{{
			Action:       awstypes.CidrCollectionChangeActionPut,
			CidrList:     fwflex.ExpandFrameworkStringValueSet(ctx, data.CIDRBlocks),
			LocationName: aws.String(name),
		}},
		CollectionVersion: collection.Version,
		Id:                aws.String(collectionID),
	}

	_, err = conn.ChangeCidrCollection(ctx, input)

	if err != nil {
		response.Diagnostics.AddError(fmt.Sprintf("creating Route 53 CIDR Location (%s)", name), err.Error())

		return
	}

	id, err := data.setID()
	if err != nil {
		response.Diagnostics.AddError(fmt.Sprintf("creating Route 53 CIDR Location (%s)", name), err.Error())
		return
	}
	data.ID = types.StringValue(id)

	response.Diagnostics.Append(response.State.Set(ctx, &data)...)
}

func (r *cidrLocationResource) Read(ctx context.Context, request resource.ReadRequest, response *resource.ReadResponse) {
	var data cidrLocationResourceModel
	response.Diagnostics.Append(request.State.Get(ctx, &data)...)
	if response.Diagnostics.HasError() {
		return
	}

	if err := data.InitFromID(); err != nil {
		response.Diagnostics.AddError("parsing resource ID", err.Error())

		return
	}

	conn := r.Meta().Route53Client(ctx)

	cidrBlocks, err := findCIDRLocationByTwoPartKey(ctx, conn, data.CIDRCollectionID.ValueString(), data.Name.ValueString())

	if tfresource.NotFound(err) {
		response.Diagnostics.Append(fwdiag.NewResourceNotFoundWarningDiagnostic(err))
		response.State.RemoveResource(ctx)

		return
	}

	if err != nil {
		response.Diagnostics.AddError(fmt.Sprintf("reading Route 53 CIDR Location (%s)", data.ID.ValueString()), err.Error())

		return
	}

	if n := len(cidrBlocks); n > 0 {
		elems := make([]attr.Value, n)
		for i, cidrBlock := range cidrBlocks {
			elems[i] = fwtypes.CIDRBlockValue(cidrBlock)
		}
		data.CIDRBlocks = fwtypes.NewSetValueOfMust[fwtypes.CIDRBlock](ctx, elems)
	} else {
		data.CIDRBlocks = fwtypes.NewSetValueOfNull[fwtypes.CIDRBlock](ctx)
	}

	response.Diagnostics.Append(response.State.Set(ctx, &data)...)
}

func (r *cidrLocationResource) Update(ctx context.Context, request resource.UpdateRequest, response *resource.UpdateResponse) {
	var old, new cidrLocationResourceModel
	response.Diagnostics.Append(request.State.Get(ctx, &old)...)
	if response.Diagnostics.HasError() {
		return
	}
	response.Diagnostics.Append(request.Plan.Get(ctx, &new)...)
	if response.Diagnostics.HasError() {
		return
	}

	conn := r.Meta().Route53Client(ctx)

	collection, err := findCIDRCollectionByID(ctx, conn, new.CIDRCollectionID.ValueString())

	if err != nil {
		response.Diagnostics.AddError(fmt.Sprintf("reading Route 53 CIDR Collection (%s)", new.CIDRCollectionID.ValueString()), err.Error())

		return
	}

	oldCIDRBlocks := fwflex.ExpandFrameworkStringValueSet(ctx, old.CIDRBlocks)
	newCIDRBlocks := fwflex.ExpandFrameworkStringValueSet(ctx, new.CIDRBlocks)
	add := newCIDRBlocks.Difference(oldCIDRBlocks)
	del := oldCIDRBlocks.Difference(newCIDRBlocks)
	collectionVersion := collection.Version

	if len(add) > 0 {
		input := &route53.ChangeCidrCollectionInput{
			Changes: []awstypes.CidrCollectionChange{{
				Action:       awstypes.CidrCollectionChangeActionPut,
				CidrList:     add,
				LocationName: new.Name.ValueStringPointer(),
			}},
			CollectionVersion: collectionVersion,
			Id:                new.CIDRCollectionID.ValueStringPointer(),
		}

		_, err = conn.ChangeCidrCollection(ctx, input)

		if err != nil {
			response.Diagnostics.AddError(fmt.Sprintf("adding CIDR blocks to Route 53 CIDR Location (%s)", new.ID.ValueString()), err.Error())

			return
		}

		collectionVersion = nil // Clear the collection version as it will have changed after the last operation.
	}

	if len(del) > 0 {
		input := &route53.ChangeCidrCollectionInput{
			Changes: []awstypes.CidrCollectionChange{{
				Action:       awstypes.CidrCollectionChangeActionDeleteIfExists,
				CidrList:     del,
				LocationName: new.Name.ValueStringPointer(),
			}},
			CollectionVersion: collectionVersion,
			Id:                new.CIDRCollectionID.ValueStringPointer(),
		}

		_, err = conn.ChangeCidrCollection(ctx, input)

		if err != nil {
			response.Diagnostics.AddError(fmt.Sprintf("removing CIDR blocks from Route 53 CIDR Location (%s)", new.ID.ValueString()), err.Error())

			return
		}
	}

	response.Diagnostics.Append(response.State.Set(ctx, &new)...)
}

func (r *cidrLocationResource) Delete(ctx context.Context, request resource.DeleteRequest, response *resource.DeleteResponse) {
	var data cidrLocationResourceModel
	response.Diagnostics.Append(request.State.Get(ctx, &data)...)
	if response.Diagnostics.HasError() {
		return
	}

	conn := r.Meta().Route53Client(ctx)

	collection, err := findCIDRCollectionByID(ctx, conn, data.CIDRCollectionID.ValueString())

	if err != nil {
		response.Diagnostics.AddError(fmt.Sprintf("reading Route 53 CIDR Collection (%s)", data.CIDRCollectionID.ValueString()), err.Error())

		return
	}

	tflog.Debug(ctx, "deleting Route 53 CIDR Location", map[string]any{
		names.AttrID: data.ID.ValueString(),
	})

	input := &route53.ChangeCidrCollectionInput{
		Changes: []awstypes.CidrCollectionChange{{
			Action:       awstypes.CidrCollectionChangeActionDeleteIfExists,
			CidrList:     fwflex.ExpandFrameworkStringValueSet(ctx, data.CIDRBlocks),
			LocationName: data.Name.ValueStringPointer(),
		}},
		CollectionVersion: collection.Version,
		Id:                data.CIDRCollectionID.ValueStringPointer(),
	}

	_, err = conn.ChangeCidrCollection(ctx, input)

	if errs.IsA[*awstypes.NoSuchCidrCollectionException](err) {
		return
	}

	if err != nil {
		response.Diagnostics.AddError(fmt.Sprintf("deleting Route 53 CIDR Location (%s)", data.ID.ValueString()), err.Error())

		return
	}
}

type cidrLocationResourceModel struct {
	CIDRBlocks       fwtypes.SetValueOf[fwtypes.CIDRBlock] `tfsdk:"cidr_blocks"`
	CIDRCollectionID types.String                          `tfsdk:"cidr_collection_id"`
	ID               types.String                          `tfsdk:"id"`
	Name             types.String                          `tfsdk:"name"`
}

const (
	cidrLocationResourceIDPartCount = 2
)

func (data *cidrLocationResourceModel) InitFromID() error {
	id := data.ID.ValueString()
	parts, err := flex.ExpandResourceId(id, cidrLocationResourceIDPartCount, false)

	if err != nil {
		return err
	}

	data.CIDRCollectionID = types.StringValue(parts[0])
	data.Name = types.StringValue(parts[1])

	return nil
}

func (data *cidrLocationResourceModel) setID() (string, error) {
	parts := []string{
		data.CIDRCollectionID.ValueString(),
		data.Name.ValueString(),
	}

	return flex.FlattenResourceId(parts, cidrLocationResourceIDPartCount, false)
}

func findCIDRLocationByTwoPartKey(ctx context.Context, conn *route53.Client, collectionID, locationName string) ([]string, error) {
	input := &route53.ListCidrBlocksInput{
		CollectionId: aws.String(collectionID),
		LocationName: aws.String(locationName),
	}
	output, err := findCIDRBlocks(ctx, conn, input)

	if len(output) == 0 {
		return nil, tfresource.NewEmptyResultError(input)
	}

	if err != nil {
		return nil, err
	}

	return tfslices.ApplyToAll(output, func(v awstypes.CidrBlockSummary) string {
		return aws.ToString(v.CidrBlock)
	}), nil
}

func findCIDRBlocks(ctx context.Context, conn *route53.Client, input *route53.ListCidrBlocksInput) ([]awstypes.CidrBlockSummary, error) {
	var output []awstypes.CidrBlockSummary

	pages := route53.NewListCidrBlocksPaginator(conn, input)
	for pages.HasMorePages() {
		page, err := pages.NextPage(ctx)

		if errs.IsA[*awstypes.NoSuchCidrCollectionException](err) || errs.IsA[*awstypes.NoSuchCidrLocationException](err) {
			return nil, &retry.NotFoundError{
				LastError:   err,
				LastRequest: input,
			}
		}

		if err != nil {
			return nil, err
		}

		output = append(output, page.CidrBlocks...)
	}

	return output, nil
}
