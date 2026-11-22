package service

import (
	"github.com/multica-ai/multica/server/internal/orgscope"
	"github.com/multica-ai/multica/server/internal/workspaceaccess"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// ProjectRequestable decides whether the caller may hand one project work.
// access is the caller's organization-workspace access, units the whole
// department tree, and departmentSources the organization's department lists.
// sources are the project's own rows; isMember reports an explicit member row
// in the project.
//
// A project list, when present, is the only answer: it does not fall back to
// the owning department, so removing someone from a configured list really
// does stop their work. A project with no rows keeps following its owning
// department, which is the behaviour every project had before lists existed.
func ProjectRequestable(
	access workspaceaccess.Access,
	units []db.ListOrgUnitsRow,
	departmentSources []db.ListOrgUnitIntakeSourcesRow,
	project db.Workspace,
	sources []db.ListWorkspaceIntakeSourcesRow,
	isMember bool,
) bool {
	if access.IsManager() || isMember {
		return true
	}
	if !project.OrgUnitID.Valid {
		return false
	}
	// Leading the owning department (or one above it) owns the project.
	if orgscope.Leads(units, access.UserID, project.OrgUnitID) {
		return true
	}
	if len(sources) == 0 {
		return canRequestToUnit(access, units, departmentSources, project.OrgUnitID)
	}
	for _, source := range sources {
		if !source.SourceID.Valid {
			continue
		}
		switch source.SourceType {
		case "user":
			if source.SourceID == access.UserID {
				return true
			}
		case "org_unit":
			if access.MemberUnit.Valid && orgscope.Subtree(units, source.SourceID)[access.MemberUnit] {
				return true
			}
			if orgscope.Leads(units, access.UserID, source.SourceID) {
				return true
			}
		}
	}
	return false
}
