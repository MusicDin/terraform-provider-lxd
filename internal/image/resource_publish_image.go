package image

import (
	"context"
	"fmt"

	lxd "github.com/canonical/lxd/client"
	"github.com/canonical/lxd/shared/api"
	"github.com/hashicorp/terraform-plugin-framework-validators/mapvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/setvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/terraform-lxd/terraform-provider-lxd/internal/common"
	"github.com/terraform-lxd/terraform-provider-lxd/internal/errors"
	provider_config "github.com/terraform-lxd/terraform-provider-lxd/internal/provider-config"
	"github.com/terraform-lxd/terraform-provider-lxd/internal/utils"
)

// PublishImageModel resource data model that matches the schema.
type PublishImageModel struct {
	Instance       types.String `tfsdk:"instance"`
	Aliases        types.Set    `tfsdk:"aliases"`
	Properties     types.Map    `tfsdk:"properties"`
	Public         types.Bool   `tfsdk:"public"`
	Filename       types.String `tfsdk:"filename"`
	CompressionAlg types.String `tfsdk:"compression_algorithm"`
	Triggers       types.List   `tfsdk:"triggers"`
	Project        types.String `tfsdk:"project"`
	Remote         types.String `tfsdk:"remote"`

	// Computed.
	Architecture types.String `tfsdk:"architecture"`
	Fingerprint  types.String `tfsdk:"fingerprint"`
	CreatedAt    types.Int64  `tfsdk:"created_at"`
}

// PublishImageResource represent LXD publish image resource.
type PublishImageResource struct {
	provider *provider_config.LxdProviderConfig
}

// NewPublishImageResource return new publish image resource.
func NewPublishImageResource() resource.Resource {
	return &PublishImageResource{}
}

func (r PublishImageResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = fmt.Sprintf("%s_publish_image", req.ProviderTypeName)
}

func (r PublishImageResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Attributes: map[string]schema.Attribute{
			"instance": schema.StringAttribute{
				Required: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},

			"aliases": schema.SetAttribute{
				Optional:    true,
				Computed:    true,
				ElementType: types.StringType,
				Default:     setdefault.StaticValue(types.SetValueMust(types.StringType, []attr.Value{})),
				Validators: []validator.Set{
					// Prevent empty values.
					setvalidator.ValueStringsAre(stringvalidator.LengthAtLeast(1)),
				},
			},

			"properties": schema.MapAttribute{
				Optional:    true,
				ElementType: types.StringType,
				Validators: []validator.Map{
					mapvalidator.KeysAre(stringvalidator.LengthAtLeast(1)),
					mapvalidator.ValueStringsAre(stringvalidator.LengthAtLeast(1)),
				},
			},

			"public": schema.BoolAttribute{
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(false),
			},

			"filename": schema.StringAttribute{
				Optional: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},

			"compression_algorithm": schema.StringAttribute{
				Optional: true,
				Computed: true,
				Default:  stringdefault.StaticString("gzip"),
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
				Validators: []validator.String{
					stringvalidator.OneOf("bzip2", "gzip", "lzma", "xz", "none"),
				},
			},

			"triggers": schema.ListAttribute{
				Optional:    true,
				ElementType: types.StringType,
				PlanModifiers: []planmodifier.List{
					listplanmodifier.RequiresReplace(),
				},
			},

			"project": schema.StringAttribute{
				Optional: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
			},

			"remote": schema.StringAttribute{
				Optional: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},

			// Computed.

			"architecture": schema.StringAttribute{
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},

			"fingerprint": schema.StringAttribute{
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},

			"created_at": schema.Int64Attribute{
				Computed: true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

func (r *PublishImageResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	data := req.ProviderData
	if data == nil {
		return
	}

	provider, ok := data.(*provider_config.LxdProviderConfig)
	if !ok {
		resp.Diagnostics.Append(errors.NewProviderDataTypeError(req.ProviderData))
		return
	}

	r.provider = provider
}

func (r PublishImageResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan PublishImageModel

	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	remote := plan.Remote.ValueString()
	project := plan.Project.ValueString()
	server, err := r.provider.InstanceServer(remote, project, "")
	if err != nil {
		resp.Diagnostics.Append(errors.NewInstanceServerError(err))
		return
	}

	instanceName := plan.Instance.ValueString()
	ct, _, err := server.GetInstanceState(instanceName)
	if err != nil { // && errors.IsNotFoundError(err)
		resp.Diagnostics.AddError(fmt.Sprintf("Failed to retrieve state of instance %q", instanceName), err.Error())
		return
	}

	if ct.StatusCode != api.Stopped {
		resp.Diagnostics.AddError(fmt.Sprintf("Cannot publish image because instance %q is running", instanceName), "")
		return
	}

	imageProps, diags := common.ToConfigMap(ctx, plan.Properties)
	resp.Diagnostics.Append(diags...)

	aliases, diags := ToAliasList(ctx, plan.Aliases)
	resp.Diagnostics.Append(diags...)

	if resp.Diagnostics.HasError() {
		return
	}

	imageAliases := make([]api.ImageAlias, 0, len(aliases))
	for _, alias := range aliases {
		// Ensure image alias does not already exist.
		aliasTarget, _, _ := server.GetImageAlias(alias)
		if aliasTarget != nil {
			resp.Diagnostics.AddError(fmt.Sprintf("Image alias %q already exists", alias), "")
			return
		}

		ia := api.ImageAlias{
			Name: alias,
		}

		imageAliases = append(imageAliases, ia)
	}

	imageReq := api.ImagesPost{
		Aliases:              imageAliases,
		Filename:             plan.Filename.ValueString(),
		CompressionAlgorithm: plan.CompressionAlg.ValueString(),
		ImagePut: api.ImagePut{
			Public:     plan.Public.ValueBool(),
			Properties: imageProps,
		},
		Source: &api.ImagesPostSource{
			Name: plan.Instance.ValueString(),
			Type: "instance",
		},
	}

	// Publish image.
	op, err := server.CreateImage(imageReq, nil)
	if err != nil {
		resp.Diagnostics.AddError(fmt.Sprintf("Failed to publish instance %q image", instanceName), err.Error())
		return
	}

	// Wait for create operation to finish.
	err = op.Wait()
	if err != nil {
		resp.Diagnostics.AddError(fmt.Sprintf("Failed to publish instance %q image", instanceName), err.Error())
		return
	}

	// Extract fingerprint from operation response.
	opResp := op.Get()
	imageFingerprint, ok := opResp.Metadata["fingerprint"].(string)
	if !ok {
		resp.Diagnostics.AddError("Failed to extract fingerprint from operation response", "")
		return
	}

	plan.Fingerprint = types.StringValue(imageFingerprint)

	// Update Terraform state.
	diags = r.SyncState(ctx, &resp.State, server, plan)
	resp.Diagnostics.Append(diags...)
}

func (r PublishImageResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state PublishImageModel

	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	remote := state.Remote.ValueString()
	project := state.Project.ValueString()
	server, err := r.provider.InstanceServer(remote, project, "")
	if err != nil {
		resp.Diagnostics.Append(errors.NewInstanceServerError(err))
		return
	}

	// Update Terraform state.
	diags = r.SyncState(ctx, &resp.State, server, state)
	resp.Diagnostics.Append(diags...)
}

func (r PublishImageResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan PublishImageModel
	var state PublishImageModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	remote := plan.Remote.ValueString()
	project := plan.Project.ValueString()
	server, err := r.provider.InstanceServer(remote, project, "")
	if err != nil {
		resp.Diagnostics.Append(errors.NewInstanceServerError(err))
		return
	}

	// Extract image fingerprint from previous state.
	imageFingerprint := state.Fingerprint.ValueString()

	imageProps, diags := common.ToConfigMap(ctx, plan.Properties)
	resp.Diagnostics.Append(diags...)

	// Get info about published image.
	image, _, err := server.GetImage(imageFingerprint)
	if err != nil {
		resp.Diagnostics.AddError("Failed to retrieve published image", err.Error())
		return
	}

	oldAliases := make([]string, len(image.Aliases))
	for i, alias := range image.Aliases {
		oldAliases[i] = alias.Name
	}

	newAliases, diags := ToAliasList(ctx, plan.Aliases)
	resp.Diagnostics.Append(diags...)

	if resp.Diagnostics.HasError() {
		return
	}

	// Extract removed and added image aliases.
	removed, added := utils.DiffSlices(oldAliases, newAliases)

	// Delete removed aliases.
	for _, alias := range removed {
		err := server.DeleteImageAlias(alias)
		if err != nil {
			resp.Diagnostics.AddError(fmt.Sprintf("Failed to delete alias %q for published image", alias), err.Error())
			return
		}
	}

	// Add new aliases.
	for _, alias := range added {
		req := api.ImageAliasesPost{}
		req.Name = alias
		req.Target = imageFingerprint

		err := server.CreateImageAlias(req)
		if err != nil {
			resp.Diagnostics.AddError(fmt.Sprintf("Failed to create alias %q for published image", alias), err.Error())
			return
		}
	}

	imageReq := api.ImagePut{
		Properties: imageProps,
		Public:     plan.Public.ValueBool(),
		ExpiresAt:  image.ExpiresAt,
		AutoUpdate: image.AutoUpdate,
		Profiles:   image.Profiles,
	}

	err = server.UpdateImage(imageFingerprint, imageReq, "")
	if err != nil {
		resp.Diagnostics.AddError("Failed to update publihsed image properties", err.Error())
		return
	}

	// Update Terraform state.
	diags = r.SyncState(ctx, &resp.State, server, plan)
	resp.Diagnostics.Append(diags...)
}

func (r PublishImageResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state PublishImageModel

	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	remote := state.Remote.ValueString()
	project := state.Project.ValueString()
	server, err := r.provider.InstanceServer(remote, project, "")
	if err != nil {
		resp.Diagnostics.Append(errors.NewInstanceServerError(err))
		return
	}

	imageFingerprint := state.Fingerprint.ValueString()
	opDelete, err := server.DeleteImage(imageFingerprint)
	if err != nil {
		resp.Diagnostics.AddError("Failed to remove published image", err.Error())
		return
	}

	err = opDelete.Wait()
	if err != nil {
		resp.Diagnostics.AddError("Failed to remove published image", err.Error())
		return
	}
}

// SyncState fetches the server's current state for a published image and
// updates the provided model. It then applies this updated model as the
// new state in Terraform.
func (r PublishImageResource) SyncState(ctx context.Context, tfState *tfsdk.State, server lxd.InstanceServer, m PublishImageModel) diag.Diagnostics {
	var respDiags diag.Diagnostics

	imageFingerprint := m.Fingerprint.ValueString()
	image, _, err := server.GetImage(imageFingerprint)
	if err != nil {
		if errors.IsNotFoundError(err) {
			tfState.RemoveResource(ctx)
			return nil
		}

		respDiags.AddError("Failed to retrieve published image", err.Error())
		return respDiags
	}

	configAliases, diags := ToAliasList(ctx, m.Aliases)
	respDiags.Append(diags...)

	// Copy aliases from image state that are present in user defined
	// config.
	var aliases []string
	for _, a := range image.Aliases {
		if utils.ValueInSlice(a.Name, configAliases) {
			aliases = append(aliases, a.Name)
		}
	}

	aliasSet, diags := ToAliasSetType(ctx, aliases)
	respDiags.Append(diags...)

	m.Fingerprint = types.StringValue(image.Fingerprint)
	m.Architecture = types.StringValue(image.Architecture)
	m.CreatedAt = types.Int64Value(image.CreatedAt.Unix())
	m.Public = types.BoolValue(image.Public)
	m.Aliases = aliasSet

	if respDiags.HasError() {
		return respDiags
	}

	return tfState.Set(ctx, m)
}
