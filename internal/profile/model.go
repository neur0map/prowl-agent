package profile

import "strings"

// Identity is a closed built-in profile identity.
type Identity string

const LocalIdentity Identity = "local"

const (
	localSoul          = "A careful local project agent."
	localSurfacePolicy = "Operate only on explicitly exposed local surfaces."
)

// Profile is an immutable built-in identity and policy. It is not a principal.
type Profile struct {
	identity      Identity
	soul          string
	surfacePolicy string
}

// Local returns the sole built-in profile.
func Local() Profile {
	return Profile{identity: LocalIdentity, soul: localSoul, surfacePolicy: localSurfacePolicy}
}

func (profile Profile) Identity() Identity    { return profile.identity }
func (profile Profile) Soul() string          { return profile.soul }
func (profile Profile) SurfacePolicy() string { return profile.surfacePolicy }

func (profile Profile) valid() bool {
	return profile.identity == LocalIdentity && profile.soul == localSoul && profile.surfacePolicy == localSurfacePolicy
}

// SourceKind is a closed source category. Authority is derived from the kind.
type SourceKind string

const (
	SystemPolicySource       SourceKind = "system_policy"
	PermissionPolicySource   SourceKind = "permission_policy"
	ProfilePolicySource      SourceKind = "profile_policy"
	UserProfileSource        SourceKind = "user_profile"
	DurableMemorySource      SourceKind = "durable_memory"
	ProjectInstructionSource SourceKind = "project_instruction"
	TaskInstructionSource    SourceKind = "task_instruction"
	UntrustedContentSource   SourceKind = "untrusted_content"
	SecretReferenceSource    SourceKind = "secret_reference"
)

// Precedence is strongest-to-weakest authority.
type Precedence string

const (
	ExecutableSystemPrecedence    Precedence = "executable_system"
	ProfilePrecedence             Precedence = "profile"
	UserProfilePrecedence         Precedence = "user_profile"
	DurableMemoryPrecedence       Precedence = "durable_memory"
	ProjectInstructionPrecedence  Precedence = "rooted_project"
	TaskInstructionPrecedence     Precedence = "task_worker"
	UntrustedContentPrecedence    Precedence = "untrusted_content"
)

func (precedence Precedence) Strength() int {
	switch precedence {
	case ExecutableSystemPrecedence:
		return 1
	case ProfilePrecedence:
		return 2
	case UserProfilePrecedence:
		return 3
	case DurableMemoryPrecedence:
		return 4
	case ProjectInstructionPrecedence:
		return 5
	case TaskInstructionPrecedence:
		return 6
	case UntrustedContentPrecedence:
		return 7
	default:
		return 0
	}
}

// Trust is a closed trust classification independent from provenance.
type Trust string

const (
	ExecutableTrust      Trust = "executable"
	ProfileTrust         Trust = "profile"
	UserTrust            Trust = "user"
	DurableTrust         Trust = "durable"
	RootedProjectTrust   Trust = "rooted_project"
	TaskTrust            Trust = "task"
	UntrustedTrust       Trust = "untrusted"
	SecretReferenceTrust Trust = "secret_reference"
)

// Provenance is a closed source origin.
type Provenance string

const (
	BuiltinProvenance       Provenance = "builtin"
	UserSelectedProvenance  Provenance = "user_selected"
	DurableMemoryProvenance Provenance = "durable_memory"
	RootedProjectProvenance Provenance = "rooted_project"
	TaskProvenance          Provenance = "task"
	WebProvenance           Provenance = "web"
	SourceProvenance        Provenance = "source"
	AttachmentProvenance    Provenance = "attachment"
	ToolOutputProvenance    Provenance = "tool_output"
	EnvironmentProvenance   Provenance = "environment"
)

// Scope is a closed applicability boundary.
type Scope string

const (
	GlobalScope    Scope = "global"
	ProfileScope   Scope = "profile"
	UserScope      Scope = "user"
	WorkspaceScope Scope = "workspace"
	TaskScope      Scope = "task"
	TurnScope      Scope = "turn"
	SessionScope   Scope = "session"
)

// Authority returns the fixed precedence and trust for a closed source kind.
func Authority(kind SourceKind) (Precedence, Trust, bool) {
	switch kind {
	case SystemPolicySource, PermissionPolicySource:
		return ExecutableSystemPrecedence, ExecutableTrust, true
	case ProfilePolicySource:
		return ProfilePrecedence, ProfileTrust, true
	case UserProfileSource:
		return UserProfilePrecedence, UserTrust, true
	case DurableMemorySource:
		return DurableMemoryPrecedence, DurableTrust, true
	case ProjectInstructionSource:
		return ProjectInstructionPrecedence, RootedProjectTrust, true
	case TaskInstructionSource:
		return TaskInstructionPrecedence, TaskTrust, true
	case UntrustedContentSource:
		return UntrustedContentPrecedence, UntrustedTrust, true
	case SecretReferenceSource:
		return ProfilePrecedence, SecretReferenceTrust, true
	default:
		return "", "", false
	}
}

func validProvenance(value Provenance) bool {
	switch value {
	case BuiltinProvenance, UserSelectedProvenance, DurableMemoryProvenance, RootedProjectProvenance, TaskProvenance, WebProvenance, SourceProvenance, AttachmentProvenance, ToolOutputProvenance, EnvironmentProvenance:
		return true
	default:
		return false
	}
}

func validScope(value Scope) bool {
	switch value {
	case GlobalScope, ProfileScope, UserScope, WorkspaceScope, TaskScope, TurnScope, SessionScope:
		return true
	default:
		return false
	}
}

// ValidSourceClassification closes provenance and scope for each authority tier.
func ValidSourceClassification(kind SourceKind, provenance Provenance, scope Scope) bool {
	switch kind {
	case SystemPolicySource:
		return provenance == BuiltinProvenance && scope == GlobalScope
	case PermissionPolicySource:
		return provenance == BuiltinProvenance && scope == SessionScope
	case ProfilePolicySource:
		return provenance == BuiltinProvenance && scope == ProfileScope
	case UserProfileSource:
		return provenance == UserSelectedProvenance && scope == UserScope
	case DurableMemorySource:
		return provenance == DurableMemoryProvenance && (scope == UserScope || scope == WorkspaceScope)
	case ProjectInstructionSource:
		return provenance == RootedProjectProvenance && scope == WorkspaceScope
	case TaskInstructionSource:
		return provenance == TaskProvenance && scope == TaskScope
	case UntrustedContentSource:
		return (provenance == WebProvenance || provenance == SourceProvenance || provenance == AttachmentProvenance || provenance == ToolOutputProvenance) && scope == TurnScope
	case SecretReferenceSource:
		return provenance == EnvironmentProvenance && scope == SessionScope
	default:
		return false
	}
}

func (value Provenance) Valid() bool { return validProvenance(value) }
func (value Scope) Valid() bool      { return validScope(value) }

func nonempty(value string) bool { return strings.TrimSpace(value) != "" }
