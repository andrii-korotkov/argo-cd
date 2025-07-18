package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/utils/ptr"

	"github.com/argoproj/argo-cd/v3/pkg/apis/application/v1alpha1"
	"github.com/argoproj/argo-cd/v3/test/e2e/fixture"
	"github.com/argoproj/argo-cd/v3/util/argo"
)

func assertProjHasEvent(t *testing.T, a *v1alpha1.AppProject, message string, reason string) {
	t.Helper()
	list, err := fixture.KubeClientset.CoreV1().Events(fixture.TestNamespace()).List(t.Context(), metav1.ListOptions{
		FieldSelector: fields.SelectorFromSet(map[string]string{
			"involvedObject.name":      a.Name,
			"involvedObject.uid":       string(a.UID),
			"involvedObject.namespace": fixture.TestNamespace(),
		}).String(),
	})
	require.NoError(t, err)

	for i := range list.Items {
		event := list.Items[i]
		if event.Reason == reason && strings.Contains(event.Message, message) {
			return
		}
	}
	t.Errorf("Unable to find event with reason=%s; message=%s", reason, message)
}

func TestProjectCreation(t *testing.T) {
	fixture.EnsureCleanState(t)

	projectName := "proj-" + fixture.Name()
	_, err := fixture.RunCli("proj", "create", projectName,
		"--description", "Test description",
		"-d", "https://192.168.99.100:8443,default",
		"-d", "https://192.168.99.100:8443,service",
		"-s", "https://github.com/argoproj/argo-cd.git",
		"--orphaned-resources")
	require.NoError(t, err)

	proj, err := fixture.AppClientset.ArgoprojV1alpha1().AppProjects(fixture.TestNamespace()).Get(t.Context(), projectName, metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, projectName, proj.Name)
	assert.Len(t, proj.Spec.Destinations, 2)

	assert.Equal(t, "https://192.168.99.100:8443", proj.Spec.Destinations[0].Server)
	assert.Equal(t, "default", proj.Spec.Destinations[0].Namespace)

	assert.Equal(t, "https://192.168.99.100:8443", proj.Spec.Destinations[1].Server)
	assert.Equal(t, "service", proj.Spec.Destinations[1].Namespace)

	assert.Len(t, proj.Spec.SourceRepos, 1)
	assert.Equal(t, "https://github.com/argoproj/argo-cd.git", proj.Spec.SourceRepos[0])

	assert.NotNil(t, proj.Spec.OrphanedResources)
	assert.False(t, proj.Spec.OrphanedResources.IsWarn())

	assertProjHasEvent(t, proj, "create", argo.EventReasonResourceCreated)

	// create a manifest with the same name to upsert
	newDescription := "Upserted description"
	proj.Spec.Description = newDescription
	proj.ResourceVersion = ""
	data, err := json.Marshal(proj)
	stdinString := string(data)
	require.NoError(t, err)

	// fail without upsert flag
	_, err = fixture.RunCliWithStdin(stdinString, false, "proj", "create",
		"-f", "-")
	require.Error(t, err)

	// succeed with the upsert flag
	_, err = fixture.RunCliWithStdin(stdinString, false, "proj", "create",
		"-f", "-", "--upsert")
	require.NoError(t, err)
	proj, err = fixture.AppClientset.ArgoprojV1alpha1().AppProjects(fixture.TestNamespace()).Get(t.Context(), projectName, metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, newDescription, proj.Spec.Description)
}

func TestProjectDeletion(t *testing.T) {
	fixture.EnsureCleanState(t)

	projectName := "proj-" + strconv.FormatInt(time.Now().Unix(), 10)
	proj, err := fixture.AppClientset.ArgoprojV1alpha1().AppProjects(fixture.TestNamespace()).Create(
		t.Context(), &v1alpha1.AppProject{ObjectMeta: metav1.ObjectMeta{Name: projectName}}, metav1.CreateOptions{})
	require.NoError(t, err)

	_, err = fixture.RunCli("proj", "delete", projectName)
	require.NoError(t, err)

	_, err = fixture.AppClientset.ArgoprojV1alpha1().AppProjects(fixture.TestNamespace()).Get(t.Context(), projectName, metav1.GetOptions{})
	assert.True(t, errors.IsNotFound(err))
	assertProjHasEvent(t, proj, "delete", argo.EventReasonResourceDeleted)
}

func TestSetProject(t *testing.T) {
	fixture.EnsureCleanState(t)

	projectName := "proj-" + strconv.FormatInt(time.Now().Unix(), 10)
	_, err := fixture.AppClientset.ArgoprojV1alpha1().AppProjects(fixture.TestNamespace()).Create(
		t.Context(), &v1alpha1.AppProject{ObjectMeta: metav1.ObjectMeta{Name: projectName}}, metav1.CreateOptions{})
	require.NoError(t, err)

	_, err = fixture.RunCli("proj", "set", projectName,
		"--description", "updated description",
		"-d", "https://192.168.99.100:8443,default",
		"-d", "https://192.168.99.100:8443,service",
		"--orphaned-resources-warn=false")
	require.NoError(t, err)

	proj, err := fixture.AppClientset.ArgoprojV1alpha1().AppProjects(fixture.TestNamespace()).Get(t.Context(), projectName, metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, projectName, proj.Name)
	assert.Len(t, proj.Spec.Destinations, 2)

	assert.Equal(t, "https://192.168.99.100:8443", proj.Spec.Destinations[0].Server)
	assert.Equal(t, "default", proj.Spec.Destinations[0].Namespace)

	assert.Equal(t, "https://192.168.99.100:8443", proj.Spec.Destinations[1].Server)
	assert.Equal(t, "service", proj.Spec.Destinations[1].Namespace)

	assert.NotNil(t, proj.Spec.OrphanedResources)
	assert.False(t, proj.Spec.OrphanedResources.IsWarn())

	assertProjHasEvent(t, proj, "update", argo.EventReasonResourceUpdated)
}

func TestAddProjectDestination(t *testing.T) {
	fixture.EnsureCleanState(t)

	projectName := "proj-" + strconv.FormatInt(time.Now().Unix(), 10)
	_, err := fixture.AppClientset.ArgoprojV1alpha1().AppProjects(fixture.TestNamespace()).Create(
		t.Context(), &v1alpha1.AppProject{ObjectMeta: metav1.ObjectMeta{Name: projectName}}, metav1.CreateOptions{})
	require.NoError(t, err, "Unable to create project")

	_, err = fixture.RunCli("proj", "add-destination", projectName,
		"https://192.168.99.100:8443",
		"test1",
	)
	require.NoError(t, err, "Unable to add project destination")

	_, err = fixture.RunCli("proj", "add-destination", projectName,
		"https://192.168.99.100:8443",
		"test1",
	)
	require.ErrorContains(t, err, "already defined")

	_, err = fixture.RunCli("proj", "add-destination", projectName,
		"!*",
		"test1",
	)
	require.ErrorContains(t, err, "server has an invalid format, '!*'")

	_, err = fixture.RunCli("proj", "add-destination", projectName,
		"https://192.168.99.100:8443",
		"!*",
	)
	require.ErrorContains(t, err, "namespace has an invalid format, '!*'")

	proj, err := fixture.AppClientset.ArgoprojV1alpha1().AppProjects(fixture.TestNamespace()).Get(t.Context(), projectName, metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, projectName, proj.Name)
	assert.Len(t, proj.Spec.Destinations, 1)

	assert.Equal(t, "https://192.168.99.100:8443", proj.Spec.Destinations[0].Server)
	assert.Equal(t, "test1", proj.Spec.Destinations[0].Namespace)
	assertProjHasEvent(t, proj, "update", argo.EventReasonResourceUpdated)
}

func TestAddProjectDestinationWithName(t *testing.T) {
	fixture.EnsureCleanState(t)

	projectName := "proj-" + strconv.FormatInt(time.Now().Unix(), 10)
	_, err := fixture.AppClientset.ArgoprojV1alpha1().AppProjects(fixture.TestNamespace()).Create(
		t.Context(), &v1alpha1.AppProject{ObjectMeta: metav1.ObjectMeta{Name: projectName}}, metav1.CreateOptions{})
	require.NoError(t, err, "Unable to create project")

	_, err = fixture.RunCli("proj", "add-destination", projectName,
		"in-cluster",
		"test1",
		"--name",
	)
	require.NoError(t, err, "Unable to add project destination")

	proj, err := fixture.AppClientset.ArgoprojV1alpha1().AppProjects(fixture.TestNamespace()).Get(t.Context(), projectName, metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, projectName, proj.Name)
	assert.Len(t, proj.Spec.Destinations, 1)

	assert.Empty(t, proj.Spec.Destinations[0].Server)
	assert.Equal(t, "in-cluster", proj.Spec.Destinations[0].Name)
	assert.Equal(t, "test1", proj.Spec.Destinations[0].Namespace)
	assertProjHasEvent(t, proj, "update", argo.EventReasonResourceUpdated)
}

func TestRemoveProjectDestination(t *testing.T) {
	fixture.EnsureCleanState(t)

	projectName := "proj-" + strconv.FormatInt(time.Now().Unix(), 10)
	_, err := fixture.AppClientset.ArgoprojV1alpha1().AppProjects(fixture.TestNamespace()).Create(t.Context(), &v1alpha1.AppProject{
		ObjectMeta: metav1.ObjectMeta{Name: projectName},
		Spec: v1alpha1.AppProjectSpec{
			Destinations: []v1alpha1.ApplicationDestination{{
				Server:    "https://192.168.99.100:8443",
				Namespace: "test",
			}},
		},
	}, metav1.CreateOptions{})
	require.NoError(t, err, "Unable to create project")

	_, err = fixture.RunCli("proj", "remove-destination", projectName,
		"https://192.168.99.100:8443",
		"test",
	)
	require.NoError(t, err, "Unable to remove project destination")

	_, err = fixture.RunCli("proj", "remove-destination", projectName,
		"https://192.168.99.100:8443",
		"test1",
	)
	require.ErrorContains(t, err, "does not exist")

	proj, err := fixture.AppClientset.ArgoprojV1alpha1().AppProjects(fixture.TestNamespace()).Get(t.Context(), projectName, metav1.GetOptions{})
	require.NoError(t, err, "Unable to get project")
	assert.Equal(t, projectName, proj.Name)
	assert.Empty(t, proj.Spec.Destinations)
	assertProjHasEvent(t, proj, "update", argo.EventReasonResourceUpdated)
}

func TestAddProjectSource(t *testing.T) {
	fixture.EnsureCleanState(t)

	projectName := "proj-" + strconv.FormatInt(time.Now().Unix(), 10)
	_, err := fixture.AppClientset.ArgoprojV1alpha1().AppProjects(fixture.TestNamespace()).Create(
		t.Context(), &v1alpha1.AppProject{ObjectMeta: metav1.ObjectMeta{Name: projectName}}, metav1.CreateOptions{})
	require.NoError(t, err, "Unable to create project")

	_, err = fixture.RunCli("proj", "add-source", projectName, "https://github.com/argoproj/argo-cd.git")
	require.NoError(t, err, "Unable to add project source")

	_, err = fixture.RunCli("proj", "add-source", projectName, "https://github.com/argoproj/argo-cd.git")
	require.NoError(t, err)

	proj, err := fixture.AppClientset.ArgoprojV1alpha1().AppProjects(fixture.TestNamespace()).Get(t.Context(), projectName, metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, projectName, proj.Name)
	assert.Len(t, proj.Spec.SourceRepos, 1)

	assert.Equal(t, "https://github.com/argoproj/argo-cd.git", proj.Spec.SourceRepos[0])
}

func TestRemoveProjectSource(t *testing.T) {
	fixture.EnsureCleanState(t)

	projectName := "proj-" + strconv.FormatInt(time.Now().Unix(), 10)
	_, err := fixture.AppClientset.ArgoprojV1alpha1().AppProjects(fixture.TestNamespace()).Create(t.Context(), &v1alpha1.AppProject{
		ObjectMeta: metav1.ObjectMeta{Name: projectName},
		Spec: v1alpha1.AppProjectSpec{
			SourceRepos: []string{"https://github.com/argoproj/argo-cd.git"},
		},
	}, metav1.CreateOptions{})

	require.NoError(t, err)

	_, err = fixture.RunCli("proj", "remove-source", projectName, "https://github.com/argoproj/argo-cd.git")

	require.NoError(t, err)

	_, err = fixture.RunCli("proj", "remove-source", projectName, "https://github.com/argoproj/argo-cd.git")
	require.NoError(t, err)

	proj, err := fixture.AppClientset.ArgoprojV1alpha1().AppProjects(fixture.TestNamespace()).Get(t.Context(), projectName, metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, projectName, proj.Name)
	assert.Empty(t, proj.Spec.SourceRepos)
	assertProjHasEvent(t, proj, "update", argo.EventReasonResourceUpdated)
}

func TestUseJWTToken(t *testing.T) {
	fixture.EnsureCleanState(t)

	projectName := "proj-" + strconv.FormatInt(time.Now().Unix(), 10)
	appName := "app-" + strconv.FormatInt(time.Now().Unix(), 10)
	roleName := "roleTest"
	roleName2 := "roleTest2"
	testApp := &v1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{
			Name: appName,
		},
		Spec: v1alpha1.ApplicationSpec{
			Source: &v1alpha1.ApplicationSource{
				RepoURL: fixture.RepoURL(fixture.RepoURLTypeFile),
				Path:    "guestbook",
			},
			Destination: v1alpha1.ApplicationDestination{
				Server:    v1alpha1.KubernetesInternalAPIServerAddr,
				Namespace: fixture.TestNamespace(),
			},
			Project: projectName,
		},
	}
	_, err := fixture.AppClientset.ArgoprojV1alpha1().AppProjects(fixture.TestNamespace()).Create(t.Context(), &v1alpha1.AppProject{
		ObjectMeta: metav1.ObjectMeta{Name: projectName},
		Spec: v1alpha1.AppProjectSpec{
			Destinations: []v1alpha1.ApplicationDestination{{
				Server:    v1alpha1.KubernetesInternalAPIServerAddr,
				Namespace: fixture.TestNamespace(),
			}},
			SourceRepos: []string{"*"},
		},
	}, metav1.CreateOptions{})
	require.NoError(t, err)

	_, err = fixture.AppClientset.ArgoprojV1alpha1().Applications(fixture.TestNamespace()).Create(t.Context(), testApp, metav1.CreateOptions{})
	require.NoError(t, err)

	_, err = fixture.RunCli("proj", "role", "create", projectName, roleName)
	require.NoError(t, err)

	roleGetResult, err := fixture.RunCli("proj", "role", "get", projectName, roleName)
	require.NoError(t, err)
	assert.True(t, strings.HasSuffix(roleGetResult, "ID  ISSUED-AT  EXPIRES-AT"))

	_, err = fixture.RunCli("proj", "role", "create-token", projectName, roleName)
	require.NoError(t, err)

	// Create second role with kubectl, to test that it will not affect 1st role
	_, err = fixture.Run("", "kubectl", "patch", "appproject", projectName, "--type", "merge",
		"-n", fixture.TestNamespace(),
		"-p", fmt.Sprintf(`{"spec":{"roles":[{"name":%q},{"name":%q}]}}`, roleName, roleName2))
	require.NoError(t, err)

	_, err = fixture.RunCli("proj", "role", "create-token", projectName, roleName2)
	require.NoError(t, err)

	for _, action := range []string{"get", "update", "sync", "create", "override", "*"} {
		_, err = fixture.RunCli("proj", "role", "add-policy", projectName, roleName, "-a", action, "-o", "*", "-p", "allow")
		require.NoError(t, err)
	}

	newProj, err := fixture.AppClientset.ArgoprojV1alpha1().AppProjects(fixture.TestNamespace()).Get(t.Context(), projectName, metav1.GetOptions{})
	require.NoError(t, err)
	assert.Len(t, newProj.Status.JWTTokensByRole[roleName].Items, 1)
	assert.ElementsMatch(t, newProj.Status.JWTTokensByRole[roleName].Items, newProj.Spec.Roles[0].JWTTokens)

	roleGetResult, err = fixture.RunCli("proj", "role", "get", projectName, roleName)
	require.NoError(t, err)
	assert.Contains(t, roleGetResult, strconv.FormatInt(newProj.Status.JWTTokensByRole[roleName].Items[0].IssuedAt, 10))

	_, err = fixture.RunCli("proj", "role", "delete-token", projectName, roleName, strconv.FormatInt(newProj.Status.JWTTokensByRole[roleName].Items[0].IssuedAt, 10))
	require.NoError(t, err)
	newProj, err = fixture.AppClientset.ArgoprojV1alpha1().AppProjects(fixture.TestNamespace()).Get(t.Context(), projectName, metav1.GetOptions{})
	require.NoError(t, err)
	assert.Nil(t, newProj.Status.JWTTokensByRole[roleName].Items)
	assert.Nil(t, newProj.Spec.Roles[0].JWTTokens)
}

func TestAddOrphanedIgnore(t *testing.T) {
	fixture.EnsureCleanState(t)

	projectName := "proj-" + strconv.FormatInt(time.Now().Unix(), 10)
	_, err := fixture.AppClientset.ArgoprojV1alpha1().AppProjects(fixture.TestNamespace()).Create(
		t.Context(), &v1alpha1.AppProject{ObjectMeta: metav1.ObjectMeta{Name: projectName}}, metav1.CreateOptions{})
	require.NoError(t, err, "Unable to create project")

	_, err = fixture.RunCli("proj", "add-orphaned-ignore", projectName,
		"group",
		"kind",
		"--name",
		"name",
	)
	require.NoError(t, err, "Unable to add resource to orphaned ignore")

	_, err = fixture.RunCli("proj", "add-orphaned-ignore", projectName,
		"group",
		"kind",
		"--name",
		"name",
	)
	require.ErrorContains(t, err, "already defined")

	proj, err := fixture.AppClientset.ArgoprojV1alpha1().AppProjects(fixture.TestNamespace()).Get(t.Context(), projectName, metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, projectName, proj.Name)
	assert.Len(t, proj.Spec.OrphanedResources.Ignore, 1)

	assert.Equal(t, "group", proj.Spec.OrphanedResources.Ignore[0].Group)
	assert.Equal(t, "kind", proj.Spec.OrphanedResources.Ignore[0].Kind)
	assert.Equal(t, "name", proj.Spec.OrphanedResources.Ignore[0].Name)
	assertProjHasEvent(t, proj, "update", argo.EventReasonResourceUpdated)
}

func TestRemoveOrphanedIgnore(t *testing.T) {
	fixture.EnsureCleanState(t)

	projectName := "proj-" + strconv.FormatInt(time.Now().Unix(), 10)
	_, err := fixture.AppClientset.ArgoprojV1alpha1().AppProjects(fixture.TestNamespace()).Create(t.Context(), &v1alpha1.AppProject{
		ObjectMeta: metav1.ObjectMeta{Name: projectName},
		Spec: v1alpha1.AppProjectSpec{
			OrphanedResources: &v1alpha1.OrphanedResourcesMonitorSettings{
				Warn:   ptr.To(true),
				Ignore: []v1alpha1.OrphanedResourceKey{{Group: "group", Kind: "kind", Name: "name"}},
			},
		},
	}, metav1.CreateOptions{})
	require.NoError(t, err, "Unable to create project")

	_, err = fixture.RunCli("proj", "remove-orphaned-ignore", projectName,
		"group",
		"kind",
		"--name",
		"name",
	)
	require.NoError(t, err, "Unable to remove resource from orphaned ignore list")

	_, err = fixture.RunCli("proj", "remove-orphaned-ignore", projectName,
		"group",
		"kind",
		"--name",
		"name",
	)
	require.ErrorContains(t, err, "does not exist")

	proj, err := fixture.AppClientset.ArgoprojV1alpha1().AppProjects(fixture.TestNamespace()).Get(t.Context(), projectName, metav1.GetOptions{})
	require.NoError(t, err, "Unable to get project")
	assert.Equal(t, projectName, proj.Name)
	assert.Empty(t, proj.Spec.OrphanedResources.Ignore)
	assertProjHasEvent(t, proj, "update", argo.EventReasonResourceUpdated)
}

func createAndConfigGlobalProject() error {
	// Create global project
	projectGlobalName := "proj-g-" + fixture.Name()
	_, err := fixture.RunCli("proj", "create", projectGlobalName,
		"--description", "Test description",
		"-d", "https://192.168.99.100:8443,default",
		"-d", "https://192.168.99.100:8443,service",
		"-s", "https://github.com/argoproj/argo-cd.git",
		"--orphaned-resources")
	if err != nil {
		return err
	}

	projGlobal, err := fixture.AppClientset.ArgoprojV1alpha1().AppProjects(fixture.TestNamespace()).Get(context.Background(), projectGlobalName, metav1.GetOptions{})
	if err != nil {
		return err
	}

	projGlobal.Spec.NamespaceResourceBlacklist = []metav1.GroupKind{
		{Group: "", Kind: "Service"},
	}

	projGlobal.Spec.NamespaceResourceWhitelist = []metav1.GroupKind{
		{Group: "", Kind: "Deployment"},
	}

	projGlobal.Spec.ClusterResourceWhitelist = []metav1.GroupKind{
		{Group: "", Kind: "Job"},
	}

	projGlobal.Spec.ClusterResourceBlacklist = []metav1.GroupKind{
		{Group: "", Kind: "Pod"},
	}

	projGlobal.Spec.SyncWindows = v1alpha1.SyncWindows{}
	win := &v1alpha1.SyncWindow{Kind: "deny", Schedule: "* * * * *", Duration: "1h", Applications: []string{"*"}}
	projGlobal.Spec.SyncWindows = append(projGlobal.Spec.SyncWindows, win)

	_, err = fixture.AppClientset.ArgoprojV1alpha1().AppProjects(fixture.TestNamespace()).Update(context.Background(), projGlobal, metav1.UpdateOptions{})
	if err != nil {
		return err
	}

	// Configure global project settings
	globalProjectsSettings := `data:
  accounts.config-service: apiKey
  globalProjects: |
    - labelSelector:
        matchExpressions:
          - key: opt
            operator: In
            values:
              - me
              - you
      projectName: %s`

	_, err = fixture.Run("", "kubectl", "patch", "cm", "argocd-cm",
		"-n", fixture.TestNamespace(),
		"-p", fmt.Sprintf(globalProjectsSettings, projGlobal.Name))
	if err != nil {
		return err
	}

	return nil
}

func TestGetVirtualProjectNoMatch(t *testing.T) {
	fixture.EnsureCleanState(t)
	err := createAndConfigGlobalProject()
	require.NoError(t, err)

	// Create project which does not match global project settings
	projectName := "proj-" + fixture.Name()
	_, err = fixture.RunCli("proj", "create", projectName,
		"--description", "Test description",
		"-d", v1alpha1.KubernetesInternalAPIServerAddr+",*",
		"-s", "*",
		"--orphaned-resources")
	require.NoError(t, err)

	proj, err := fixture.AppClientset.ArgoprojV1alpha1().AppProjects(fixture.TestNamespace()).Get(t.Context(), projectName, metav1.GetOptions{})
	require.NoError(t, err)

	// Create an app belongs to proj project
	_, err = fixture.RunCli("app", "create", fixture.Name(), "--repo", fixture.RepoURL(fixture.RepoURLTypeFile),
		"--path", guestbookPath, "--project", proj.Name, "--dest-server", v1alpha1.KubernetesInternalAPIServerAddr, "--dest-namespace", fixture.DeploymentNamespace())
	require.NoError(t, err)

	// App trying to sync a resource which is not blacked listed anywhere
	_, err = fixture.RunCli("app", "sync", fixture.Name(), "--resource", "apps:Deployment:guestbook-ui", "--timeout", strconv.Itoa(10))
	require.NoError(t, err)

	// app trying to sync a resource which is black listed by global project
	_, err = fixture.RunCli("app", "sync", fixture.Name(), "--resource", ":Service:guestbook-ui", "--timeout", strconv.Itoa(10))
	require.NoError(t, err)
}

func TestGetVirtualProjectMatch(t *testing.T) {
	fixture.EnsureCleanState(t)
	err := createAndConfigGlobalProject()
	require.NoError(t, err)

	// Create project which matches global project settings
	projectName := "proj-" + fixture.Name()
	_, err = fixture.RunCli("proj", "create", projectName,
		"--description", "Test description",
		"-d", v1alpha1.KubernetesInternalAPIServerAddr+",*",
		"-s", "*",
		"--orphaned-resources")
	require.NoError(t, err)

	proj, err := fixture.AppClientset.ArgoprojV1alpha1().AppProjects(fixture.TestNamespace()).Get(t.Context(), projectName, metav1.GetOptions{})
	require.NoError(t, err)

	// Add a label to this project so that this project match global project selector
	proj.Labels = map[string]string{"opt": "me"}
	_, err = fixture.AppClientset.ArgoprojV1alpha1().AppProjects(fixture.TestNamespace()).Update(t.Context(), proj, metav1.UpdateOptions{})
	require.NoError(t, err)

	// Create an app belongs to proj project
	_, err = fixture.RunCli("app", "create", fixture.Name(), "--repo", fixture.RepoURL(fixture.RepoURLTypeFile),
		"--path", guestbookPath, "--project", proj.Name, "--dest-server", v1alpha1.KubernetesInternalAPIServerAddr, "--dest-namespace", fixture.DeploymentNamespace())
	require.NoError(t, err)

	// App trying to sync a resource which is not blacked listed anywhere
	_, err = fixture.RunCli("app", "sync", fixture.Name(), "--resource", "apps:Deployment:guestbook-ui", "--timeout", strconv.Itoa(10))
	require.ErrorContains(t, err, "blocked by sync window")

	// app trying to sync a resource which is black listed by global project
	_, err = fixture.RunCli("app", "sync", fixture.Name(), "--resource", ":Service:guestbook-ui", "--timeout", strconv.Itoa(10))
	assert.ErrorContains(t, err, "blocked by sync window")
}

func TestAddProjectDestinationServiceAccount(t *testing.T) {
	fixture.EnsureCleanState(t)

	projectName := "proj-" + strconv.FormatInt(time.Now().Unix(), 10)
	_, err := fixture.AppClientset.ArgoprojV1alpha1().AppProjects(fixture.TestNamespace()).Create(
		t.Context(), &v1alpha1.AppProject{ObjectMeta: metav1.ObjectMeta{Name: projectName}}, metav1.CreateOptions{})
	require.NoError(t, err, "Unable to create project")

	// Given, an existing project
	// When, a default destination service account with all valid fields is added to it,
	// Then, there is no error.
	_, err = fixture.RunCli("proj", "add-destination-service-account", projectName,
		"https://192.168.99.100:8443",
		"test-ns",
		"test-sa",
	)
	require.NoError(t, err, "Unable to add project destination service account")

	// Given, an existing project
	// When, a default destination service account with empty namespace is added to it,
	// Then, there is no error.
	_, err = fixture.RunCli("proj", "add-destination-service-account", projectName,
		"https://192.168.99.100:8443",
		"",
		"test-sa",
	)
	require.NoError(t, err, "Unable to add project destination service account")

	// Given, an existing project,
	// When, a default destination service account is added with a custom service account namespace,
	// Then, there is no error.
	_, err = fixture.RunCli("proj", "add-destination-service-account", projectName,
		"https://192.168.99.100:8443",
		"test-ns1",
		"test-sa",
		"--service-account-namespace",
		"default",
	)
	require.NoError(t, err, "Unable to add project destination service account")

	// Given, an existing project,
	// When, a duplicate default destination service account is added,
	// Then, there is an error with appropriate message.
	_, err = fixture.RunCli("proj", "add-destination-service-account", projectName,
		"https://192.168.99.100:8443",
		"test-ns",
		"test-sa",
	)
	require.ErrorContains(t, err, "already defined")

	// Given, an existing project,
	// When, a duplicate default destination service account is added,
	// Then, there is an error with appropriate message.
	_, err = fixture.RunCli("proj", "add-destination-service-account", projectName,
		"https://192.168.99.100:8443",
		"test-ns",
		"asdf",
	)
	require.ErrorContains(t, err, "already added")

	// Given, an existing project,
	// When, a default destination service account with negation glob pattern for server is added,
	// Then, there is an error with appropriate message.
	_, err = fixture.RunCli("proj", "add-destination-service-account", projectName,
		"!*",
		"test-ns",
		"test-sa",
	)
	require.ErrorContains(t, err, "server has an invalid format, '!*'")

	// Given, an existing project,
	// When, a default destination service account with negation glob pattern for server is added,
	// Then, there is an error with appropriate message.
	_, err = fixture.RunCli("proj", "add-destination-service-account", projectName,
		"!abc",
		"test-ns",
		"test-sa",
	)
	require.ErrorContains(t, err, "server has an invalid format, '!abc'")

	// Given, an existing project,
	// When, a default destination service account with negation glob pattern for namespace is added,
	// Then, there is an error with appropriate message.
	_, err = fixture.RunCli("proj", "add-destination-service-account", projectName,
		"https://192.168.99.100:8443",
		"!*",
		"test-sa",
	)
	require.ErrorContains(t, err, "namespace has an invalid format, '!*'")

	// Given, an existing project,
	// When, a default destination service account with negation glob pattern for namespace is added,
	// Then, there is an error with appropriate message.
	_, err = fixture.RunCli("proj", "add-destination-service-account", projectName,
		"https://192.168.99.100:8443",
		"!abc",
		"test-sa",
	)
	require.ErrorContains(t, err, "namespace has an invalid format, '!abc'")

	// Given, an existing project,
	// When, a default destination service account with empty service account is added,
	// Then, there is an error with appropriate message.
	_, err = fixture.RunCli("proj", "add-destination-service-account", projectName,
		"https://192.168.99.100:8443",
		"test-ns",
		"",
	)
	require.ErrorContains(t, err, "defaultServiceAccount has an invalid format, ''")

	// Given, an existing project,
	// When, a default destination service account with service account having just white spaces is added,
	// Then, there is an error with appropriate message.
	_, err = fixture.RunCli("proj", "add-destination-service-account", projectName,
		"https://192.168.99.100:8443",
		"test-ns",
		"   ",
	)
	require.ErrorContains(t, err, "defaultServiceAccount has an invalid format, '   '")

	// Given, an existing project,
	// When, a default destination service account with service account having backwards slash char is added,
	// Then, there is an error with appropriate message.
	_, err = fixture.RunCli("proj", "add-destination-service-account", projectName,
		"https://192.168.99.100:8443",
		"test-ns",
		"test\\sa",
	)
	require.ErrorContains(t, err, "defaultServiceAccount has an invalid format, 'test\\\\sa'")

	// Given, an existing project,
	// When, a default destination service account with service account having forward slash char is added,
	// Then, there is an error with appropriate message.
	_, err = fixture.RunCli("proj", "add-destination-service-account", projectName,
		"https://192.168.99.100:8443",
		"test-ns",
		"test/sa",
	)
	require.ErrorContains(t, err, "defaultServiceAccount has an invalid format, 'test/sa'")

	// Given, an existing project,
	// When, a default destination service account with service account having square braces char is added,
	// Then, there is an error with appropriate message.
	_, err = fixture.RunCli("proj", "add-destination-service-account", projectName,
		"https://192.168.99.100:8443",
		"test-ns",
		"[test-sa]",
	)
	require.ErrorContains(t, err, "defaultServiceAccount has an invalid format, '[test-sa]'")

	// Given, an existing project,
	// When, a default destination service account with service account having curly braces char is added,
	// Then, there is an error with appropriate message.
	_, err = fixture.RunCli("proj", "add-destination-service-account", projectName,
		"https://192.168.99.100:8443",
		"test-ns",
		"{test-sa}",
	)
	require.ErrorContains(t, err, "defaultServiceAccount has an invalid format, '{test-sa}'")

	// Given, an existing project,
	// When, a default destination service account with service account having curly braces char is added,
	// Then, there is an error with appropriate message.
	_, err = fixture.RunCli("proj", "add-destination-service-account", projectName,
		"[[ech*",
		"test-ns",
		"test-sa",
	)
	require.ErrorContains(t, err, "server has an invalid format, '[[ech*'")

	// Given, an existing project,
	// When, a default destination service account with service account having curly braces char is added,
	// Then, there is an error with appropriate message.
	_, err = fixture.RunCli("proj", "add-destination-service-account", projectName,
		"https://192.168.99.100:8443",
		"[[ech*",
		"test-sa",
	)
	require.ErrorContains(t, err, "namespace has an invalid format, '[[ech*'")

	proj, err := fixture.AppClientset.ArgoprojV1alpha1().AppProjects(fixture.TestNamespace()).Get(t.Context(), projectName, metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, projectName, proj.Name)
	assert.Len(t, proj.Spec.DestinationServiceAccounts, 3)

	assert.Equal(t, "https://192.168.99.100:8443", proj.Spec.DestinationServiceAccounts[0].Server)
	assert.Equal(t, "test-ns", proj.Spec.DestinationServiceAccounts[0].Namespace)
	assert.Equal(t, "test-sa", proj.Spec.DestinationServiceAccounts[0].DefaultServiceAccount)

	assert.Equal(t, "https://192.168.99.100:8443", proj.Spec.DestinationServiceAccounts[1].Server)
	assert.Empty(t, proj.Spec.DestinationServiceAccounts[1].Namespace)
	assert.Equal(t, "test-sa", proj.Spec.DestinationServiceAccounts[1].DefaultServiceAccount)

	assert.Equal(t, "https://192.168.99.100:8443", proj.Spec.DestinationServiceAccounts[2].Server)
	assert.Equal(t, "test-ns1", proj.Spec.DestinationServiceAccounts[2].Namespace)
	assert.Equal(t, "default:test-sa", proj.Spec.DestinationServiceAccounts[2].DefaultServiceAccount)

	assertProjHasEvent(t, proj, "update", argo.EventReasonResourceUpdated)
}
