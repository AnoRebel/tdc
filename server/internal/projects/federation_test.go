package projects_test

import (
	"context"
	"errors"
	"testing"

	"github.com/AnoRebel/tdc/server/internal/authz"
	"github.com/AnoRebel/tdc/server/internal/projects"
)

/*
Somebody on another server can be in a project, with a role, only once the
project is opened to federation, and only an administrator here decides it.
*/
func TestAProjectHoldsPeopleFromOtherServers(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	amina := f.person(t, "amina")
	bola := f.person(t, "bola")
	access := f.project(t, amina, "SHR")
	if err := f.projects.SetMember(ctx, amina, access, "bola", "member"); err != nil {
		t.Fatal(err)
	}

	if err := f.projects.SetMember(ctx, amina, access, "dalila@elsewhere.example", "member"); !errors.Is(err, projects.ErrNotOpened) {
		t.Fatalf("a project not opened took a member from elsewhere: %v", err)
	}

	theirs, err := f.projects.ProjectByID(ctx, bola, access.Project.ID)
	if err != nil {
		t.Fatal(err)
	}
	var refusal projects.RefusalError
	if err := f.projects.SetFederated(ctx, bola, theirs, true); !errors.As(err, &refusal) {
		t.Fatalf("a member who does not administer the project opened it: %v", err)
	}
	if err := f.projects.SetFederated(ctx, amina, access, true); err != nil {
		t.Fatal(err)
	}
	if access, err = f.projects.ProjectByID(ctx, amina, access.Project.ID); err != nil {
		t.Fatal(err)
	}
	stream, err := f.projects.Stream(ctx, access.Project.ID)
	if err != nil {
		t.Fatal(err)
	}
	length := len(stream)
	if err := f.projects.SetFederated(ctx, amina, access, true); err != nil {
		t.Fatal(err)
	}
	if stream, _ = f.projects.Stream(ctx, access.Project.ID); len(stream) != length {
		t.Error("opening a project already open recorded a change")
	}

	if theirs, err = f.projects.ProjectByID(ctx, bola, access.Project.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.projects.SetMember(ctx, bola, theirs, "dalila@elsewhere.example", "member"); !errors.As(err, &refusal) {
		t.Fatalf("a member who does not administer the project added somebody: %v", err)
	}
	for _, malformed := range []string{"@elsewhere.example", "dalila@", "da lila@elsewhere.example"} {
		if err := f.projects.SetMember(ctx, amina, access, malformed, "member"); !errors.Is(err, projects.ErrRemoteAddress) {
			t.Errorf("%q was taken as an address: %v", malformed, err)
		}
	}

	if err := f.projects.SetMember(ctx, amina, access, "Dalila@Elsewhere.Example", "admin"); err != nil {
		t.Fatal(err)
	}
	if err := f.projects.SetMember(ctx, amina, access, "dalila@elsewhere.example", "member"); err != nil {
		t.Fatal(err)
	}
	remote, err := f.projects.RemoteMembers(ctx, access)
	if err != nil {
		t.Fatal(err)
	}
	if len(remote) != 1 || remote[0].Address != "dalila@elsewhere.example" || remote[0].Role != "member" {
		t.Fatalf("remote members are %+v, want dalila@elsewhere.example as a member", remote)
	}

	// This server's own name is somebody here, not a second membership.
	if err := f.projects.SetMember(ctx, amina, access, "bola@"+testServerName, "admin"); err != nil {
		t.Fatal(err)
	}
	if theirs, err = f.projects.ProjectByID(ctx, bola, access.Project.ID); err != nil || theirs.Role != "admin" {
		t.Fatalf("bola@%s did not name bola here: role %q, %v", testServerName, theirs.Role, err)
	}
	if remote, _ = f.projects.RemoteMembers(ctx, access); len(remote) != 1 {
		t.Errorf("naming somebody here by address made them a remote member too: %+v", remote)
	}

	if err := f.projects.RemoveMember(ctx, amina, access, "dalila@elsewhere.example"); err != nil {
		t.Fatal(err)
	}
	if err := f.projects.RemoveMember(ctx, amina, access, "dalila@elsewhere.example"); !errors.Is(err, projects.ErrNotAMember) {
		t.Errorf("removing somebody twice: %v", err)
	}
}

/*
A remote member may do what their project role allows and nothing more, and is
refused exactly as somebody here with the same role would be — naming the
permission. A project not opened, or one they are not in, does not exist as far
as a request on their behalf can tell.
*/
func TestARemoteMemberIsHeldToTheirProjectRole(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	amina := f.person(t, "amina")
	bola := f.person(t, "bola")
	access := f.project(t, amina, "ROL")
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(f.projects.SetMember(ctx, amina, access, "bola", "member"))
	must(f.projects.SetFederated(ctx, amina, access, true))
	access, err := f.projects.ProjectByID(ctx, amina, access.Project.ID)
	must(err)
	must(f.projects.SetMember(ctx, amina, access, "dalila@elsewhere.example", "member"))
	must(f.projects.SetMember(ctx, amina, access, "esi@elsewhere.example", "admin"))

	if _, err := f.projects.RemoteAccess(ctx, access.Project.ID, "nobody@elsewhere.example"); !errors.Is(err, projects.ErrProjectNotFound) {
		t.Errorf("somebody not in the project was answered: %v", err)
	}

	dalila, err := f.projects.RemoteAccess(ctx, access.Project.ID, "dalila@elsewhere.example")
	must(err)
	local, err := f.projects.ProjectByID(ctx, bola, access.Project.ID)
	must(err)
	for _, permission := range []authz.Permission{authz.PermIssueTransition, authz.PermIssueCreate} {
		if !dalila.Allows(permission).Allowed {
			t.Errorf("a remote member may not %s, which their role allows", permission)
		}
	}
	for _, permission := range []authz.Permission{authz.PermProjectAdminister, authz.PermIssueDelete} {
		remote, here := dalila.Allows(permission), local.Allows(permission)
		if remote.Allowed || remote != here {
			t.Errorf("%s: a remote member was answered %+v, somebody here with the same role %+v", permission, remote, here)
		}
		if remote.Permission != permission {
			t.Errorf("the refusal names %q, not %q", remote.Permission, permission)
		}
	}
	esi, err := f.projects.RemoteAccess(ctx, access.Project.ID, "esi@elsewhere.example")
	must(err)
	if !esi.Allows(authz.PermIssueDelete).Allowed {
		t.Error("a remote administrator may not delete issues")
	}

	must(f.projects.SetFederated(ctx, amina, access, false))
	if _, err := f.projects.RemoteAccess(ctx, access.Project.ID, "dalila@elsewhere.example"); !errors.Is(err, projects.ErrProjectNotFound) {
		t.Errorf("a project closed to federation still answered for its remote member: %v", err)
	}
}
