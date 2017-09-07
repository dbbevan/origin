package restrictusers

import (
	"fmt"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apiserver/pkg/admission"
	"k8s.io/apiserver/pkg/authentication/user"
	kapi "k8s.io/kubernetes/pkg/api"
	"k8s.io/kubernetes/pkg/apis/rbac"
	kadmission "k8s.io/kubernetes/pkg/kubeapiserver/admission"
	//kcache "k8s.io/client-go/tools/cache"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/kubernetes/pkg/client/clientset_generated/internalclientset/fake"

	authorizationapi "github.com/openshift/origin/pkg/authorization/apis/authorization"
	otestclient "github.com/openshift/origin/pkg/client/testclient"
	oadmission "github.com/openshift/origin/pkg/cmd/server/admission"
	//"github.com/openshift/origin/pkg/project/cache"
	userapi "github.com/openshift/origin/pkg/user/apis/user"
)

func TestAdmission(t *testing.T) {
	var (
		userAlice = userapi.User{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "Alice",
				Labels: map[string]string{"foo": "bar"},
			},
		}
		userAliceSubj = rbac.Subject{
			Kind: rbac.UserKind,
			Name: "Alice",
		}

		userBob = userapi.User{
			ObjectMeta: metav1.ObjectMeta{Name: "Bob"},
			Groups:     []string{"group"},
		}
		userBobSubj = rbac.Subject{
			Kind: rbac.UserKind,
			Name: "Bob",
		}

		group = userapi.Group{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "group",
				Labels: map[string]string{"baz": "quux"},
			},
			Users: []string{userBobSubj.Name},
		}
		groupSubj = rbac.Subject{
			Kind: rbac.GroupKind,
			Name: "group",
		}

		serviceaccount = kapi.ServiceAccount{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "namespace",
				Name:      "serviceaccount",
				Labels:    map[string]string{"xyzzy": "thud"},
			},
		}
		serviceaccountSubj = rbac.Subject{
			Kind:      rbac.ServiceAccountKind,
			Namespace: "namespace",
			Name:      "serviceaccount",
		}
	)

	testCases := []struct {
		name        string
		expectedErr string

		object      runtime.Object
		oldObject   runtime.Object
		kind        schema.GroupVersionKind
		resource    schema.GroupVersionResource
		namespace   string
		subresource string
		objects     []runtime.Object
	}{
		{
			name: "ignore (allow) if subresource is nonempty",
			object: &rbac.RoleBinding{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "namespace",
					Name:      "rolebinding",
				},
				Subjects: []rbac.Subject{userAliceSubj},
			},
			oldObject: &rbac.RoleBinding{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "namespace",
					Name:      "rolebinding",
				},
				Subjects: []rbac.Subject{},
			},
			kind:        rbac.Kind("RoleBinding").WithVersion("version"),
			resource:    rbac.Resource("rolebindings").WithVersion("version"),
			namespace:   "namespace",
			subresource: "subresource",
			objects: []runtime.Object{
				&kapi.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: "namespace",
					},
				},
			},
		},
		{
			name: "ignore (allow) cluster-scoped rolebinding",
			object: &rbac.RoleBinding{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "namespace",
					Name:      "rolebinding",
				},
				Subjects: []rbac.Subject{userAliceSubj},
				RoleRef:  rbac.RoleRef{Name: "name"},
			},
			oldObject: &rbac.RoleBinding{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "namespace",
					Name:      "rolebinding",
				},
				Subjects: []rbac.Subject{},
			},
			kind:        rbac.Kind("RoleBinding").WithVersion("version"),
			resource:    rbac.Resource("rolebindings").WithVersion("version"),
			namespace:   "",
			subresource: "",
			objects: []runtime.Object{
				&kapi.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: "namespace",
					},
				},
			},
		},
		{
			name: "allow if the namespace has no rolebinding restrictions",
			object: &rbac.RoleBinding{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "namespace",
					Name:      "rolebinding",
				},
				Subjects: []rbac.Subject{
					userAliceSubj,
					userBobSubj,
					groupSubj,
					serviceaccountSubj,
				},
				RoleRef: rbac.RoleRef{Name: "name"},
			},
			oldObject: &rbac.RoleBinding{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "namespace",
					Name:      "rolebinding",
				},
				Subjects: []rbac.Subject{},
				RoleRef:  rbac.RoleRef{Name: "name"},
			},
			kind:        rbac.Kind("RoleBinding").WithVersion("version"),
			resource:    rbac.Resource("rolebindings").WithVersion("version"),
			namespace:   "namespace",
			subresource: "",
			objects: []runtime.Object{
				&kapi.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: "namespace",
					},
				},
			},
		},
		{
			name: "allow if any rolebinding with the subject already exists",
			object: &rbac.RoleBinding{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "namespace",
					Name:      "rolebinding",
				},
				Subjects: []rbac.Subject{
					userAliceSubj,
					groupSubj,
					serviceaccountSubj,
				},
				RoleRef: rbac.RoleRef{Name: "name"},
			},
			oldObject: &rbac.RoleBinding{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "namespace",
					Name:      "rolebinding",
				},
				Subjects: []rbac.Subject{
					userAliceSubj,
					groupSubj,
					serviceaccountSubj,
				},
				RoleRef: rbac.RoleRef{Name: "name"},
			},
			kind:        rbac.Kind("RoleBinding").WithVersion("version"),
			resource:    rbac.Resource("rolebindings").WithVersion("version"),
			namespace:   "namespace",
			subresource: "",
			objects: []runtime.Object{
				&kapi.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: "namespace",
					},
				},
				&authorizationapi.RoleBindingRestriction{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "bogus-matcher",
						Namespace: "namespace",
					},
					Spec: authorizationapi.RoleBindingRestrictionSpec{
						UserRestriction: &authorizationapi.UserRestriction{},
					},
				},
			},
		},
		{
			name: "allow a user, group, or service account in a rolebinding if a literal matches",
			object: &rbac.RoleBinding{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "namespace",
					Name:      "rolebinding",
				},
				Subjects: []rbac.Subject{
					userAliceSubj,
					serviceaccountSubj,
					groupSubj,
				},
				RoleRef: rbac.RoleRef{Name: "name"},
			},
			oldObject: &rbac.RoleBinding{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "namespace",
					Name:      "rolebinding",
				},
				Subjects: []rbac.Subject{},
				RoleRef:  rbac.RoleRef{Name: "name"},
			},
			kind:        rbac.Kind("RoleBinding").WithVersion("version"),
			resource:    rbac.Resource("rolebindings").WithVersion("version"),
			namespace:   "namespace",
			subresource: "",
			objects: []runtime.Object{
				&kapi.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: "namespace",
					},
				},
				&authorizationapi.RoleBindingRestriction{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "match-users",
						Namespace: "namespace",
					},
					Spec: authorizationapi.RoleBindingRestrictionSpec{
						UserRestriction: &authorizationapi.UserRestriction{
							Users: []string{userAlice.Name},
						},
					},
				},
				&authorizationapi.RoleBindingRestriction{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "match-groups",
						Namespace: "namespace",
					},
					Spec: authorizationapi.RoleBindingRestrictionSpec{
						GroupRestriction: &authorizationapi.GroupRestriction{
							Groups: []string{group.Name},
						},
					},
				},
				&authorizationapi.RoleBindingRestriction{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "match-serviceaccounts",
						Namespace: "namespace",
					},
					Spec: authorizationapi.RoleBindingRestrictionSpec{
						ServiceAccountRestriction: &authorizationapi.ServiceAccountRestriction{
							ServiceAccounts: []authorizationapi.ServiceAccountReference{
								{
									Name:      serviceaccount.Name,
									Namespace: serviceaccount.Namespace,
								},
							},
						},
					},
				},
			},
		},
		{
			name: "prohibit user without a matching user literal",
			expectedErr: fmt.Sprintf("rolebindings to %s %q are not allowed",
				userAliceSubj.Kind, userAliceSubj.Name),
			object: &rbac.RoleBinding{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "namespace",
					Name:      "rolebinding",
				},
				Subjects: []rbac.Subject{
					userAliceSubj,
				},
				RoleRef: rbac.RoleRef{Name: "name"},
			},
			oldObject: &rbac.RoleBinding{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "namespace",
					Name:      "rolebinding",
				},
				Subjects: []rbac.Subject{},
				RoleRef:  rbac.RoleRef{Name: "name"},
			},
			kind:        rbac.Kind("RoleBinding").WithVersion("version"),
			resource:    rbac.Resource("rolebindings").WithVersion("version"),
			namespace:   "namespace",
			subresource: "",
			objects: []runtime.Object{
				&kapi.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: "namespace",
					},
				},
				&userAlice,
				&userBob,
				&authorizationapi.RoleBindingRestriction{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "match-users-bob",
						Namespace: "namespace",
					},
					Spec: authorizationapi.RoleBindingRestrictionSpec{
						UserRestriction: &authorizationapi.UserRestriction{
							Users: []string{userBobSubj.Name},
						},
					},
				},
			},
		},
	}

	stopCh := make(chan struct{})
	defer close(stopCh)

	for _, tc := range testCases {
		kclientset := fake.NewSimpleClientset(otestclient.UpstreamObjects(tc.objects)...)
		oclient := otestclient.NewSimpleFake(otestclient.OriginObjects(tc.objects)...)

		plugin, err := NewRestrictUsersAdmission()
		if err != nil {
			t.Errorf("unexpected error initializing admission plugin: %v", err)
		}

		plugin.(kadmission.WantsInternalKubeClientSet).SetInternalKubeClientSet(kclientset)
		plugin.(oadmission.WantsDeprecatedOpenshiftClient).SetDeprecatedOpenshiftClient(oclient)
		plugin.(*restrictUsersAdmission).groupCache = fakeGroupCache{}

		err = admission.Validate(plugin)
		if err != nil {
			t.Errorf("unexpected error validating admission plugin: %v", err)
		}

		attributes := admission.NewAttributesRecord(
			tc.object,
			tc.oldObject,
			tc.kind,
			tc.namespace,
			tc.name,
			tc.resource,
			tc.subresource,
			admission.Create,
			&user.DefaultInfo{},
		)

		err = plugin.Admit(attributes)
		switch {
		case len(tc.expectedErr) == 0 && err == nil:
		case len(tc.expectedErr) == 0 && err != nil:
			t.Errorf("%s: unexpected error: %v", tc.name, err)
		case len(tc.expectedErr) != 0 && err == nil:
			t.Errorf("%s: missing error: %v", tc.name, tc.expectedErr)
		case len(tc.expectedErr) != 0 && err != nil &&
			!strings.Contains(err.Error(), tc.expectedErr):
			t.Errorf("%s: missing error: expected %v, got %v",
				tc.name, tc.expectedErr, err)
		}
	}
}
