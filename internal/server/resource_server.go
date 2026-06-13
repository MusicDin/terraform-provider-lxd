package server

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"

	lxd "github.com/canonical/lxd/client"
	"github.com/canonical/lxd/shared/api"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/mapdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/terraform-lxd/terraform-provider-lxd/internal/common"
	"github.com/terraform-lxd/terraform-provider-lxd/internal/errors"
	provider_config "github.com/terraform-lxd/terraform-provider-lxd/internal/provider-config"
)

// metadataConfigExtension is the API extension that exposes the server
// configuration metadata used to validate and classify config keys.
const metadataConfigExtension = "metadata_configuration"

// ServerModel represents LXD server resource.
type ServerModel struct {
	Remote types.String `tfsdk:"remote"`
	Config types.Map    `tfsdk:"config"`
}

// ServerResource represents LXD server resource.
type ServerResource struct {
	provider *provider_config.LxdProviderConfig
}

// NewServerResource returns a new server resource.
func NewServerResource() resource.Resource {
	return &ServerResource{}
}

func (r ServerResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_server"
}

func (r ServerResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Attributes: map[string]schema.Attribute{
			"remote": schema.StringAttribute{
				Optional: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},

			// Contains global LXD server configuration keys that are
			// managed by Terraform. Only keys present in this map are
			// tracked and modified. Member-specific (local) configuration
			// keys are not supported and are rejected.
			"config": schema.MapAttribute{
				Optional:    true,
				Computed:    true,
				ElementType: types.StringType,
				Default:     mapdefault.StaticValue(types.MapValueMust(types.StringType, map[string]attr.Value{})),
			},
		},
	}
}

func (r *ServerResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r ServerResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan ServerModel

	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	remote := plan.Remote.ValueString()
	server, err := r.provider.InstanceServer(remote, "", "")
	if err != nil {
		resp.Diagnostics.Append(errors.NewInstanceServerError(err))
		return
	}

	if err := requireMetadataConfigExtension(server); err != nil {
		resp.Diagnostics.AddError("Unsupported LXD server", err.Error())
		return
	}

	planConfig, diags := common.ToConfigMap(ctx, plan.Config)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := validateServerConfigKeys(server, planConfig); err != nil {
		resp.Diagnostics.AddError("Invalid server configuration", err.Error())
		return
	}

	apiServer, etag, err := server.GetServer()
	if err != nil {
		resp.Diagnostics.AddError("Failed to retrieve LXD server configuration", err.Error())
		return
	}

	// Overlay the user-managed keys on top of the existing server
	// configuration so unrelated settings are never touched.
	newConfig := maps.Clone(apiServer.Config)
	for k, v := range planConfig {
		newConfig[k] = v
	}

	err = server.UpdateServer(api.ServerPut{Config: newConfig}, etag)
	if err != nil {
		resp.Diagnostics.AddError("Failed to update LXD server configuration", err.Error())
		return
	}

	diags = r.SyncState(ctx, &resp.State, server, plan)
	resp.Diagnostics.Append(diags...)
}

func (r ServerResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state ServerModel

	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	remote := state.Remote.ValueString()
	server, err := r.provider.InstanceServer(remote, "", "")
	if err != nil {
		resp.Diagnostics.Append(errors.NewInstanceServerError(err))
		return
	}

	diags = r.SyncState(ctx, &resp.State, server, state)
	resp.Diagnostics.Append(diags...)
}

func (r ServerResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state ServerModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	remote := plan.Remote.ValueString()
	server, err := r.provider.InstanceServer(remote, "", "")
	if err != nil {
		resp.Diagnostics.Append(errors.NewInstanceServerError(err))
		return
	}

	if err := requireMetadataConfigExtension(server); err != nil {
		resp.Diagnostics.AddError("Unsupported LXD server", err.Error())
		return
	}

	planConfig, diags := common.ToConfigMap(ctx, plan.Config)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	stateConfig, diags := common.ToConfigMap(ctx, state.Config)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := validateServerConfigKeys(server, planConfig); err != nil {
		resp.Diagnostics.AddError("Invalid server configuration", err.Error())
		return
	}

	apiServer, etag, err := server.GetServer()
	if err != nil {
		resp.Diagnostics.AddError("Failed to retrieve LXD server configuration", err.Error())
		return
	}

	newConfig := maps.Clone(apiServer.Config)
	for k, v := range planConfig {
		newConfig[k] = v
	}

	// Keys that were tracked in state but removed from the plan are reset
	// back to their default (empty) value. All other existing server
	// configuration is left untouched.
	for k := range stateConfig {
		if _, ok := planConfig[k]; !ok {
			newConfig[k] = ""
		}
	}

	err = server.UpdateServer(api.ServerPut{Config: newConfig}, etag)
	if err != nil {
		resp.Diagnostics.AddError("Failed to update LXD server configuration", err.Error())
		return
	}

	diags = r.SyncState(ctx, &resp.State, server, plan)
	resp.Diagnostics.Append(diags...)
}

func (r ServerResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state ServerModel

	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	remote := state.Remote.ValueString()
	server, err := r.provider.InstanceServer(remote, "", "")
	if err != nil {
		resp.Diagnostics.Append(errors.NewInstanceServerError(err))
		return
	}

	stateConfig, diags := common.ToConfigMap(ctx, state.Config)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	if len(stateConfig) == 0 {
		return
	}

	apiServer, etag, err := server.GetServer()
	if err != nil {
		resp.Diagnostics.AddError("Failed to retrieve LXD server configuration", err.Error())
		return
	}

	// Reset only the keys that were managed by this resource back to their
	// default (empty) value. All other existing server configuration is
	// left untouched.
	newConfig := maps.Clone(apiServer.Config)
	for k := range stateConfig {
		newConfig[k] = ""
	}

	err = server.UpdateServer(api.ServerPut{Config: newConfig}, etag)
	if err != nil {
		resp.Diagnostics.AddError("Failed to update LXD server configuration", err.Error())
	}
}

func (r ServerResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("remote"), req.ID)...)
}

// SyncState fetches the LXD server's current configuration and updates the
// provided model, keeping only the keys that are tracked by the model's
// "config" attribute. It then applies this updated model as the new state
// in Terraform.
func (r ServerResource) SyncState(ctx context.Context, tfState *tfsdk.State, server lxd.InstanceServer, m ServerModel) diag.Diagnostics {
	var respDiags diag.Diagnostics

	apiServer, _, err := server.GetServer()
	if err != nil {
		respDiags.AddError("Failed to retrieve LXD server configuration", err.Error())
		return respDiags
	}

	liveConfig := serverConfigToStringMap(apiServer.Config)

	trackedConfig, diags := common.ToConfigMap(ctx, m.Config)
	respDiags.Append(diags...)
	if respDiags.HasError() {
		return respDiags
	}

	config := make(map[string]string, len(trackedConfig))
	for k := range trackedConfig {
		// A missing key means the value was reset to its default (empty).
		config[k] = liveConfig[k]
	}

	configValue, diags := types.MapValueFrom(ctx, types.StringType, config)
	respDiags.Append(diags...)
	if respDiags.HasError() {
		return respDiags
	}

	m.Config = configValue

	return tfState.Set(ctx, &m)
}

// requireMetadataConfigExtension returns an error if the LXD server does not
// support the metadata configuration API extension. This resource relies on
// that extension to classify configuration keys and must not be used without
// it.
func requireMetadataConfigExtension(server lxd.InstanceServer) error {
	if server.CheckExtension(metadataConfigExtension) != nil {
		return fmt.Errorf("LXD server does not support the %q API extension, which is required to manage server configuration", metadataConfigExtension)
	}

	return nil
}

// validateServerConfigKeys ensures that all keys in config are valid global
// (cluster-wide) server configuration keys, as reported by the server's
// metadata configuration. Member-specific (local) configuration keys are not
// yet supported by this resource and are rejected.
func validateServerConfigKeys(server lxd.InstanceServer, config map[string]string) error {
	globalKeys, localKeys, err := serverConfigKeys(server)
	if err != nil {
		return err
	}

	for k := range config {
		if slices.Contains(localKeys, k) {
			return fmt.Errorf("Config key %q is a member-specific configuration key, which is not supported by this resource", k)
		}

		if !slices.Contains(globalKeys, k) {
			return fmt.Errorf("Config key %q is not a valid global server configuration key", k)
		}
	}

	return nil
}

// serverConfigKeys returns the list of global (cluster-wide) and
// member-specific (local) server configuration keys, derived from the LXD
// server's metadata configuration. Read-only "volatile." keys are excluded
// from both lists.
func serverConfigKeys(server lxd.InstanceServer) (globalKeys []string, localKeys []string, err error) {
	apiServer, _, err := server.GetServer()
	if err != nil {
		return nil, nil, err
	}

	meta, err := common.ServerMetadataConfiguration(apiServer.Environment.ServerVersion, server)
	if err != nil {
		return nil, nil, err
	}

	serverConfigs, ok := meta.Configs["server"]
	if !ok {
		return nil, nil, fmt.Errorf("Metadata configuration does not contain a %q section", "server")
	}

	for _, group := range serverConfigs {
		for _, keys := range group.Keys {
			for k, v := range keys {
				if strings.HasPrefix(k, "volatile.") {
					continue
				}

				if v.Scope == "local" {
					localKeys = append(localKeys, k)
					continue
				}

				globalKeys = append(globalKeys, k)
			}
		}
	}

	return globalKeys, localKeys, nil
}

// serverConfigToStringMap converts a LXD server configuration map into a
// map[string]string. Non-string values are not expected, but are converted
// using their default string representation to avoid losing data.
func serverConfigToStringMap(config map[string]any) map[string]string {
	result := make(map[string]string, len(config))

	for k, v := range config {
		s, ok := v.(string)
		if !ok {
			s = fmt.Sprintf("%v", v)
		}

		result[k] = s
	}

	return result
}
