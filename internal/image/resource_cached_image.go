package image

import (
	"context"
	"fmt"
	"slices"
	"strings"

	lxd "github.com/canonical/lxd/client"
	"github.com/canonical/lxd/shared/api"
	"github.com/hashicorp/terraform-plugin-framework-validators/setvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/terraform-lxd/terraform-provider-lxd/internal/errors"
	provider_config "github.com/terraform-lxd/terraform-provider-lxd/internal/provider-config"
	"github.com/terraform-lxd/terraform-provider-lxd/internal/utils"
)

// CachedImageModel resource data model that matches the schema.
type CachedImageModel struct {
	Description  types.String `tfsdk:"description"`
	SourceImage  types.String `tfsdk:"source_image"`
	SourceRemote types.String `tfsdk:"source_remote"`
	Type         types.String `tfsdk:"type"`
	Aliases      types.Set    `tfsdk:"aliases"`
	CopyAliases  types.Bool   `tfsdk:"copy_aliases"`
	AutoUpdate   types.Bool   `tfsdk:"auto_update"`
	Public       types.Bool   `tfsdk:"public"`
	Project      types.String `tfsdk:"project"`
	Remote       types.String `tfsdk:"remote"`

	// Computed.
	Architecture  types.String `tfsdk:"architecture"`
	CreatedAt     types.Int64  `tfsdk:"created_at"`
	Fingerprint   types.String `tfsdk:"fingerprint"`
	CopiedAliases types.Set    `tfsdk:"copied_aliases"`
	Tracker       types.String `tfsdk:"tracker"`
}

// CachedImageResource represent LXD cached image resource.
type CachedImageResource struct {
	provider *provider_config.LxdProviderConfig
}

// NewCachedImageResource return new cached image resource.
func NewCachedImageResource() resource.Resource {
	return &CachedImageResource{}
}

func (r CachedImageResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = fmt.Sprintf("%s_cached_image", req.ProviderTypeName)
}

func (r CachedImageResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Attributes: map[string]schema.Attribute{
			"description": schema.StringAttribute{
				Optional: true,
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},

			"source_image": schema.StringAttribute{
				Required: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},

			"source_remote": schema.StringAttribute{
				Required: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},

			"type": schema.StringAttribute{
				Optional: true,
				Computed: true,
				Default:  stringdefault.StaticString("container"),
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
				Validators: []validator.String{
					stringvalidator.OneOf("container", "virtual-machine"),
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

			"copy_aliases": schema.BoolAttribute{
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(false),
				PlanModifiers: []planmodifier.Bool{
					boolplanmodifier.RequiresReplace(),
				},
			},

			"auto_update": schema.BoolAttribute{
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(false),
			},

			"public": schema.BoolAttribute{
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(false),
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

			// Computed attributes.

			"architecture": schema.StringAttribute{
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

			"fingerprint": schema.StringAttribute{
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},

			"copied_aliases": schema.SetAttribute{
				Computed:    true,
				ElementType: types.StringType,
				PlanModifiers: []planmodifier.Set{
					setplanmodifier.UseStateForUnknown(),
				},
			},

			"tracker": schema.StringAttribute{
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

func (r *CachedImageResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r CachedImageResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan CachedImageModel

	// Fetch resource model from Terraform plan.
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

	imageName := plan.SourceImage.ValueString()
	imageType := plan.Type.ValueString()
	imageRemote := plan.SourceRemote.ValueString()
	imageServer, err := r.provider.ImageServer(imageRemote)
	if err != nil {
		resp.Diagnostics.Append(errors.NewImageServerError(err))
		return
	}

	// Determine whether the user has provided an fingerprint or an alias.
	aliasTarget, _, _ := imageServer.GetImageAliasType(imageType, imageName)
	if aliasTarget != nil {
		imageName = aliasTarget.Target
	}

	aliases, diags := ToAliasList(ctx, plan.Aliases)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	imageAliases := make([]api.ImageAlias, 0, len(aliases))
	for _, alias := range aliases {
		imageAlias := api.ImageAlias{
			Name: alias,
		}

		imageAliases = append(imageAliases, imageAlias)
	}

	// Get data about remote image (also checks if image exists).
	imageInfo, etag, err := imageServer.GetImage(imageName)
	if err != nil {
		resp.Diagnostics.AddError(fmt.Sprintf("Failed to retrieve info about image %q", imageName), err.Error())
		return
	}

	if plan.CopyAliases.ValueBool() {
		// Copy only image aliases that are not already defined by the user.
		for _, imageAlias := range imageInfo.Aliases {
			if !slices.Contains(aliases, imageAlias.Name) {
				imageAliases = append(imageAliases, imageAlias)
			}
		}
	}

	// Copy image.
	args := lxd.ImageCopyArgs{
		Aliases:    imageAliases,
		AutoUpdate: plan.AutoUpdate.ValueBool(),
		Public:     plan.Public.ValueBool(),
	}

	opCopy, err := server.CopyImage(imageServer, *imageInfo, &args)
	if err != nil {
		resp.Diagnostics.AddError(fmt.Sprintf("Failed to copy image %q", imageName), err.Error())
		return
	}

	// Wait for copy operation to finish.
	err = opCopy.Wait()
	if err != nil {
		resp.Diagnostics.AddError(fmt.Sprintf("Failed to copy image %q", imageName), err.Error())
		return
	}

	// Fetch metadata of the copied image.
	image, etag, err := server.GetImage(imageInfo.Fingerprint)
	if err != nil {
		resp.Diagnostics.AddError(fmt.Sprintf("Failed to retireve copied image %q", imageName), err.Error())
		return
	}

	// Update copied image.
	newImage := image.Writable()

	if !plan.Description.IsNull() {
		newImage.Properties["description"] = plan.Description.ValueString()
	}

	err = server.UpdateImage(imageInfo.Fingerprint, newImage, etag)
	if err != nil {
		resp.Diagnostics.AddError(fmt.Sprintf("Failed to update image %q", imageName), err.Error())
		return
	}

	// Store remote aliases that we've copied, so we can filter them
	// out later.
	copied := make([]string, 0)
	if plan.CopyAliases.ValueBool() {
		for _, a := range imageInfo.Aliases {
			copied = append(copied, a.Name)
		}
	}

	copiedAliases, diags := types.SetValueFrom(ctx, types.StringType, copied)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	plan.Fingerprint = types.StringValue(imageInfo.Fingerprint)
	plan.CopiedAliases = copiedAliases

	// Update Terraform state.
	diags = r.SyncState(ctx, &resp.State, server, plan)
	resp.Diagnostics.Append(diags...)
}

func (r CachedImageResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state CachedImageModel

	// Fetch resource model from Terraform state.
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

func (r CachedImageResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan CachedImageModel
	var state CachedImageModel

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

	// Extract imageName metadata (fingerprint is retained from previous state).
	imageName := plan.SourceImage.ValueString()
	imageFingerprint := state.Fingerprint.ValueString()

	// Get info about cached image.
	image, etag, err := server.GetImage(imageFingerprint)
	if err != nil {
		resp.Diagnostics.AddError(fmt.Sprintf("Failed to retrieve cached image %q", imageName), err.Error())
		return
	}

	if !plan.Description.IsNull() {
		image.Properties["description"] = plan.Description.ValueString()
	}

	// Update cached image.
	newImage := api.ImagePut{
		AutoUpdate: plan.AutoUpdate.ValueBool(),
		Public:     plan.Public.ValueBool(),
		Properties: image.Properties,
		ExpiresAt:  image.ExpiresAt,
		Profiles:   image.Profiles,
	}

	err = server.UpdateImage(imageFingerprint, newImage, etag)
	if err != nil {
		resp.Diagnostics.AddError(fmt.Sprintf("Failed to update image %q", imageName), err.Error())
		return
	}

	// Parse current (old) image aliases.
	oldAliases := make([]string, len(image.Aliases))
	for i, alias := range image.Aliases {
		oldAliases[i] = alias.Name
	}

	// Parse expected (new) image aliases.
	copiedAliases := make([]string, 0, len(plan.CopiedAliases.Elements()))
	diags := req.State.GetAttribute(ctx, path.Root("copied_aliases"), &copiedAliases)
	resp.Diagnostics.Append(diags...)

	newAliases, diags := ToAliasList(ctx, plan.Aliases)
	resp.Diagnostics.Append(diags...)

	newAliases = slices.Compact(append(newAliases, copiedAliases...))

	if resp.Diagnostics.HasError() {
		return
	}

	// Extract removed and added image aliases.
	removed, added := utils.DiffSlices(oldAliases, newAliases)

	// Delete removed aliases.
	for _, alias := range removed {
		err := server.DeleteImageAlias(alias)
		if err != nil {
			resp.Diagnostics.AddError(fmt.Sprintf("Failed to delete alias %q for cached image %q", alias, imageName), err.Error())
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
			resp.Diagnostics.AddError(fmt.Sprintf("Failed to create alias %q for cached image %q", alias, imageName), err.Error())
			return
		}
	}

	plan.Fingerprint = types.StringValue(imageFingerprint)

	// Update Terraform state.
	diags = r.SyncState(ctx, &resp.State, server, plan)
	resp.Diagnostics.Append(diags...)
}

func (r CachedImageResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state CachedImageModel

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
		resp.Diagnostics.AddError(fmt.Sprintf("Failed to remove cached image %q", state.SourceImage.ValueString()), err.Error())
		return
	}

	err = opDelete.Wait()
	if err != nil {
		resp.Diagnostics.AddError(fmt.Sprintf("Failed to remove cached image %q", state.SourceImage.ValueString()), err.Error())
		return
	}
}

// SyncState fetches the server's current state for a cached image and
// updates the provided model. It then applies this updated model as the
// new state in Terraform.
func (r CachedImageResource) SyncState(ctx context.Context, tfState *tfsdk.State, server lxd.InstanceServer, m CachedImageModel) diag.Diagnostics {
	var respDiags diag.Diagnostics

	imageFingerprint := m.Fingerprint.ValueString()
	imageName := m.SourceImage.ValueString()
	image, _, err := server.GetImage(imageFingerprint)
	if err != nil {
		if errors.IsNotFoundError(err) {
			image = nil

			// If image is not found, it could be bacuse the image was auto updated
			// which causes the image fingerprint to change. If an image Tracker is
			// stored for that image, try to find an image that matches by URL and
			// alias.
			url, alias, ok := fromImageTracker(m.Tracker.ValueString())
			if !ok {
				// No valid tracker, remove image from state.
				tfState.RemoveResource(ctx)
				return nil
			}

			images, err := server.GetImages()
			if err != nil {
				respDiags.AddError("Failed to retrieve cached images", err.Error())
				return respDiags
			}

			for _, img := range images {
				imgSource := image.UpdateSource
				if imgSource != nil &&
					imgSource.Server == url &&
					imgSource.Alias == alias &&
					imgSource.Protocol == "simplestreams" {
					// Image matches all cached image information.
					image = &img
				}
			}

			// Image not found.
			tfState.RemoveResource(ctx)
			return nil
		}

		if image == nil {
			respDiags.AddError(fmt.Sprintf("Failed to retrieve cached image %q", imageName), err.Error())
			return respDiags
		}
	}

	configAliases, diags := ToAliasList(ctx, m.Aliases)
	respDiags.Append(diags...)

	copiedAliases, diags := ToAliasList(ctx, m.CopiedAliases)
	respDiags.Append(diags...)

	// Extract aliases from image that are either present in user defined
	// config or are not copied from initial remote image.
	var aliases []string
	for _, a := range image.Aliases {
		if utils.ValueInSlice(a.Name, configAliases) || !utils.ValueInSlice(a.Name, copiedAliases) {
			aliases = append(aliases, a.Name)
		}
	}

	aliasSet, diags := ToAliasSetType(ctx, aliases)
	respDiags.Append(diags...)

	m.Description = types.StringValue(image.Properties["description"])
	m.Fingerprint = types.StringValue(image.Fingerprint)
	m.Architecture = types.StringValue(image.Architecture)
	m.AutoUpdate = types.BoolValue(image.AutoUpdate)
	m.Public = types.BoolValue(image.Public)
	m.CreatedAt = types.Int64Value(image.CreatedAt.Unix())
	m.Tracker = types.StringValue(toImageTracker(image.UpdateSource))
	m.Aliases = aliasSet

	if respDiags.HasError() {
		return respDiags
	}

	return tfState.Set(ctx, &m)
}

// ToAliasList converts aliases of type types.Set into a slice of strings.
func ToAliasList(ctx context.Context, aliasSet types.Set) ([]string, diag.Diagnostics) {
	if aliasSet.IsNull() || aliasSet.IsUnknown() {
		return []string{}, nil
	}

	aliases := make([]string, 0, len(aliasSet.Elements()))
	diags := aliasSet.ElementsAs(ctx, &aliases, false)
	return aliases, diags
}

// ToAliasSetType converts slice of strings into aliases of type types.Set.
func ToAliasSetType(ctx context.Context, aliases []string) (types.Set, diag.Diagnostics) {
	if len(aliases) == 0 {
		// Prevent null value if slice is empty.
		return types.SetValueMust(types.StringType, []attr.Value{}), nil
	}

	return types.SetValueFrom(ctx, types.StringType, aliases)
}

// toImageTracker converts image source server and alias into a tracker.
func toImageTracker(source *api.ImageSource) string {
	if source == nil || source.Server == "" || source.Alias == "" {
		return ""
	}

	return fmt.Sprintf("%s|%s", source.Server, source.Alias)
}

// toImageTracker converts the tracker into image server and alias.
func fromImageTracker(tracker string) (server string, alias string, ok bool) {
	parts := strings.SplitN(tracker, "|", 2)
	if len(parts) < 2 {
		return "", "", false
	}

	server = parts[0]
	alias = parts[1]

	if server == "" || alias == "" {
		return "", "", false
	}

	return server, alias, true
}
