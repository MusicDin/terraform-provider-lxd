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
	Remote          types.String `tfsdk:"remote"`
	Config          types.Map    `tfsdk:"config"`
	MemberOverrides types.Map    `tfsdk:"member_overrides"`
	Members         types.Map    `tfsdk:"members"`
}

// ServerMemberModel represents a per-member server configuration.
type ServerMemberModel struct {
	Config types.Map `tfsdk:"config"`
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

			// Contains global server configuration keys, as well as
			// member-specific (local) keys that act as defaults applied to
			// all cluster members, unless overridden in "member_overrides".
			// Only keys present in this map are tracked and modified.
			"config": schema.MapAttribute{
				Optional:    true,
				Computed:    true,
				ElementType: types.StringType,
				Default:     mapdefault.StaticValue(types.MapValueMust(types.StringType, map[string]attr.Value{})),
			},

			// Contains member-specific (local) configuration overrides.
			// Overrides take precedence over the local defaults defined in
			// "config". Only allowed when LXD is clustered.
			"member_overrides": schema.MapNestedAttribute{
				Optional: true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"config": schema.MapAttribute{
							Optional:    true,
							ElementType: types.StringType,
						},
					},
				},
			},

			// Contains the resolved member-specific (local) configuration
			// for all cluster members, limited to the keys tracked via
			// "config" (local defaults) and "member_overrides".
			"members": schema.MapNestedAttribute{
				Computed: true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"config": schema.MapAttribute{
							Computed:    true,
							ElementType: types.StringType,
						},
					},
				},
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

func (r ServerResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.Plan.Raw.IsNull() {
		// Nothing to do on destroy.
		return
	}

	var plan ServerModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Cannot resolve members if config or member_overrides are not yet known.
	if plan.Config.IsUnknown() || plan.MemberOverrides.IsUnknown() {
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

	_, memberConfigs, err := plan.ParseServerConfigs(ctx, server)
	if err != nil {
		resp.Diagnostics.AddError("Failed to parse server configuration", err.Error())
		return
	}

	membersValue, diags := toServerMembersTypeFromConfigs(ctx, memberConfigs)
	if diags.HasError() {
		resp.Diagnostics.Append(diags...)
		return
	}

	resp.Plan.SetAttribute(ctx, path.Root("members"), membersValue)
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

	globalConfig, memberConfigs, err := plan.ParseServerConfigs(ctx, server)
	if err != nil {
		resp.Diagnostics.AddError("Failed to parse server configuration", err.Error())
		return
	}

	if err := applyServerConfig(server, globalConfig, nil); err != nil {
		resp.Diagnostics.AddError("Failed to update LXD server configuration", err.Error())
		return
	}

	for name, memberConfig := range memberConfigs {
		if err := applyServerConfig(server.UseTarget(name), memberConfig, nil); err != nil {
			resp.Diagnostics.AddError(fmt.Sprintf("Failed to update LXD server configuration on member %q", name), err.Error())
			return
		}
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

	if err := requireMetadataConfigExtension(server); err != nil {
		resp.Diagnostics.AddError("Unsupported LXD server", err.Error())
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

	planGlobalConfig, planMemberConfigs, err := plan.ParseServerConfigs(ctx, server)
	if err != nil {
		resp.Diagnostics.AddError("Failed to parse server configuration", err.Error())
		return
	}

	stateGlobalConfig, stateMemberConfigs, err := state.ParseServerConfigs(ctx, server)
	if err != nil {
		resp.Diagnostics.AddError("Failed to parse previous server configuration", err.Error())
		return
	}

	if err := applyServerConfig(server, planGlobalConfig, stateGlobalConfig); err != nil {
		resp.Diagnostics.AddError("Failed to update LXD server configuration", err.Error())
		return
	}

	memberNames := make(map[string]bool, len(planMemberConfigs))
	for name := range planMemberConfigs {
		memberNames[name] = true
	}

	for name := range stateMemberConfigs {
		memberNames[name] = true
	}

	for name := range memberNames {
		err := applyServerConfig(server.UseTarget(name), planMemberConfigs[name], stateMemberConfigs[name])
		if err != nil {
			resp.Diagnostics.AddError(fmt.Sprintf("Failed to update LXD server configuration on member %q", name), err.Error())
			return
		}
	}

	diags := r.SyncState(ctx, &resp.State, server, plan)
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

	if err := requireMetadataConfigExtension(server); err != nil {
		resp.Diagnostics.AddError("Unsupported LXD server", err.Error())
		return
	}

	stateGlobalConfig, stateMemberConfigs, err := state.ParseServerConfigs(ctx, server)
	if err != nil {
		resp.Diagnostics.AddError("Failed to parse server configuration", err.Error())
		return
	}

	if err := applyServerConfig(server, nil, stateGlobalConfig); err != nil {
		resp.Diagnostics.AddError("Failed to update LXD server configuration", err.Error())
		return
	}

	for name, memberConfig := range stateMemberConfigs {
		if err := applyServerConfig(server.UseTarget(name), nil, memberConfig); err != nil {
			resp.Diagnostics.AddError(fmt.Sprintf("Failed to update LXD server configuration on member %q", name), err.Error())
			return
		}
	}
}

func (r ServerResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("remote"), req.ID)...)
}

// SyncState fetches the LXD server's current configuration and updates the
// provided model, keeping only the keys that are tracked by the model's
// "config" and "member_overrides" attributes. It then applies this updated
// model as the new state in Terraform.
func (r ServerResource) SyncState(ctx context.Context, tfState *tfsdk.State, server lxd.InstanceServer, m ServerModel) diag.Diagnostics {
	var respDiags diag.Diagnostics

	apiServer, _, err := server.GetServer()
	if err != nil {
		respDiags.AddError("Failed to retrieve LXD server configuration", err.Error())
		return respDiags
	}

	_, localKeys, err := serverConfigKeys(apiServer, server)
	if err != nil {
		respDiags.AddError("Failed to retrieve LXD server configuration metadata", err.Error())
		return respDiags
	}

	liveConfig := serverConfigToStringMap(apiServer.Config)

	trackedConfig, diags := common.ToConfigMap(ctx, m.Config)
	respDiags.Append(diags...)
	if respDiags.HasError() {
		return respDiags
	}

	config := make(map[string]string, len(trackedConfig))
	for k, v := range trackedConfig {
		if apiServer.Environment.ServerClustered && slices.Contains(localKeys, k) {
			// Local defaults cannot be read back from a clustered server:
			// each member's actual value is reflected in "members" instead.
			config[k] = v
			continue
		}

		config[k] = liveConfig[k]
	}

	configValue, diags := types.MapValueFrom(ctx, types.StringType, config)
	respDiags.Append(diags...)
	if respDiags.HasError() {
		return respDiags
	}

	// Resolve the per-member configuration keys that are tracked, based on
	// the model's "config" (local defaults) and "member_overrides".
	_, trackedMemberConfigs, err := m.ParseServerConfigs(ctx, server)
	if err != nil {
		respDiags.AddError("Failed to parse server configuration", err.Error())
		return respDiags
	}

	members := make(map[string]ServerMemberModel, len(trackedMemberConfigs))
	for name, trackedMemberConfig := range trackedMemberConfigs {
		memberServer := server.UseTarget(name)

		memberAPIServer, _, err := memberServer.GetServer()
		if err != nil {
			respDiags.AddError(fmt.Sprintf("Failed to retrieve LXD server configuration for member %q", name), err.Error())
			return respDiags
		}

		memberLiveConfig := serverConfigToStringMap(memberAPIServer.Config)

		memberConfig := make(map[string]string, len(trackedMemberConfig))
		for k := range trackedMemberConfig {
			memberConfig[k] = memberLiveConfig[k]
		}

		memberConfigValue, diags := types.MapValueFrom(ctx, types.StringType, memberConfig)
		respDiags.Append(diags...)
		if respDiags.HasError() {
			return respDiags
		}

		members[name] = ServerMemberModel{Config: memberConfigValue}
	}

	membersValue, diags := toServerMembersType(ctx, members)
	respDiags.Append(diags...)
	if respDiags.HasError() {
		return respDiags
	}

	m.Config = configValue
	m.Members = membersValue

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

// ParseServerConfigs splits the model's "config" into global server
// configuration and, for clustered servers, per-member local configuration.
//
// For non-clustered servers, the returned member configuration is nil and
// "config" (which may contain both global and local-scope keys) should be
// applied as-is; LXD assigns local-scope keys to the single server
// automatically. "member_overrides" is not allowed in this case.
//
// For clustered servers, local-scope keys in "config" act as defaults applied
// to every cluster member, unless overridden for a specific member via
// "member_overrides". The returned global configuration contains only
// global-scope keys.
func (m ServerModel) ParseServerConfigs(ctx context.Context, server lxd.InstanceServer) (globalConfig map[string]string, memberConfigs map[string]map[string]string, err error) {
	config, diags := common.ToConfigMap(ctx, m.Config)
	err = errors.FromDiagnostics(diags)
	if err != nil {
		return nil, nil, fmt.Errorf("Unable to convert server config to map: %v", err)
	}

	apiServer, _, err := server.GetServer()
	if err != nil {
		return nil, nil, err
	}

	globalKeys, localKeys, err := serverConfigKeys(apiServer, server)
	if err != nil {
		return nil, nil, err
	}

	for k := range config {
		if !slices.Contains(globalKeys, k) && !slices.Contains(localKeys, k) {
			return nil, nil, fmt.Errorf("Config key %q is not a valid server configuration key", k)
		}
	}

	hasMemberOverrides := len(m.MemberOverrides.Elements()) > 0

	if !apiServer.Environment.ServerClustered {
		if hasMemberOverrides {
			return nil, nil, fmt.Errorf("Server configuration \"member_overrides\" is allowed only when LXD is clustered")
		}

		// Return early with the full config. LXD assigns global and
		// local-scope keys appropriately on a non-clustered server.
		return config, nil, nil
	}

	memberNames, err := server.GetClusterMemberNames()
	if err != nil {
		return nil, nil, err
	}

	// Separate global and local (member-specific) configuration.
	globalConfig = make(map[string]string)
	localDefaults := make(map[string]string)
	for k, v := range config {
		if slices.Contains(localKeys, k) {
			localDefaults[k] = v
		} else {
			globalConfig[k] = v
		}
	}

	// Apply local defaults to all cluster members.
	memberConfigs = make(map[string]map[string]string, len(memberNames))
	for _, name := range memberNames {
		memberConfigs[name] = maps.Clone(localDefaults)
	}

	// Apply member-specific config overrides.
	overrides := map[string]ServerMemberModel{}
	err = errors.FromDiagnostics(m.MemberOverrides.ElementsAs(ctx, &overrides, true))
	if err != nil {
		return nil, nil, fmt.Errorf("Unable to extract member-specific config overrides: %v", err)
	}

	for name, override := range overrides {
		memberConfig, ok := memberConfigs[name]
		if !ok {
			return nil, nil, fmt.Errorf("Server configuration contains \"member_overrides\" for a non-existent cluster member %q", name)
		}

		overrideMap, diags := common.ToConfigMap(ctx, override.Config)
		err = errors.FromDiagnostics(diags)
		if err != nil {
			return nil, nil, fmt.Errorf("Unable to convert member-specific config override to map: %v", err)
		}

		for k := range overrideMap {
			if !slices.Contains(localKeys, k) {
				return nil, nil, fmt.Errorf("Invalid config key %q for server member %q: only member-specific configuration keys are allowed in \"member_overrides\"", k, name)
			}
		}

		maps.Copy(memberConfig, overrideMap)
		memberConfigs[name] = memberConfig
	}

	return globalConfig, memberConfigs, nil
}

// serverConfigKeys returns the list of global (cluster-wide) and
// member-specific (local) server configuration keys, derived from the LXD
// server's metadata configuration. Read-only "volatile." keys are excluded
// from both lists.
func serverConfigKeys(apiServer *api.Server, server lxd.InstanceServer) (globalKeys []string, localKeys []string, err error) {
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

// applyServerConfig overlays planConfig on top of the server's current
// configuration, resets any keys present in stateConfig but absent from
// planConfig back to their default (empty) value, and applies the result.
// All other existing server configuration is left untouched. If both
// planConfig and stateConfig are empty, no request is made.
func applyServerConfig(server lxd.InstanceServer, planConfig map[string]string, stateConfig map[string]string) error {
	if len(planConfig) == 0 && len(stateConfig) == 0 {
		return nil
	}

	apiServer, etag, err := server.GetServer()
	if err != nil {
		return err
	}

	newConfig := maps.Clone(apiServer.Config)
	for k, v := range planConfig {
		newConfig[k] = v
	}

	for k := range stateConfig {
		if _, ok := planConfig[k]; !ok {
			newConfig[k] = ""
		}
	}

	return server.UpdateServer(api.ServerPut{Config: newConfig}, etag)
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

// toServerMembersType converts a map of per-member configuration into the
// types.Map representation used by the "members" attribute.
func toServerMembersType(ctx context.Context, members map[string]ServerMemberModel) (types.Map, diag.Diagnostics) {
	memberObjType := types.ObjectType{AttrTypes: map[string]attr.Type{
		"config": types.MapType{ElemType: types.StringType},
	}}

	if members == nil {
		members = map[string]ServerMemberModel{}
	}

	return types.MapValueFrom(ctx, memberObjType, members)
}

// toServerMembersType converts a map of per-member configuration (as plain
// string maps) into the types.Map representation used by the "members"
// attribute.
func toServerMembersTypeFromConfigs(ctx context.Context, memberConfigs map[string]map[string]string) (types.Map, diag.Diagnostics) {
	members := make(map[string]ServerMemberModel, len(memberConfigs))
	for name, config := range memberConfigs {
		configValue, diags := types.MapValueFrom(ctx, types.StringType, config)
		if diags.HasError() {
			return types.MapNull(types.ObjectType{AttrTypes: map[string]attr.Type{
				"config": types.MapType{ElemType: types.StringType},
			}}), diags
		}

		members[name] = ServerMemberModel{Config: configValue}
	}

	return toServerMembersType(ctx, members)
}
