/*
Copyright 2016 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package server

import (
	"context"
	"fmt"
	"io"
	"net"

	"github.com/spf13/cobra"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/version"
	"k8s.io/apiserver/pkg/endpoints/openapi"
	"k8s.io/kube-openapi/pkg/common"
	spec "k8s.io/kube-openapi/pkg/validation/spec"
	genericapiserver "k8s.io/apiserver/pkg/server"
	genericoptions "k8s.io/apiserver/pkg/server/options"
	"k8s.io/apiserver/pkg/util/compatibility"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	basecompatibility "k8s.io/component-base/compatibility"
	"k8s.io/component-base/featuregate"
	baseversion "k8s.io/component-base/version"
	"k8s.io/sample-apiserver/pkg/apis/providerceph/v1alpha1"
	"k8s.io/sample-apiserver/pkg/apiserver"
	informers "k8s.io/sample-apiserver/pkg/generated/informers/externalversions"
	sampleopenapi "k8s.io/sample-apiserver/pkg/generated/openapi"
	netutils "k8s.io/utils/net"
)

const defaultEtcdPathPrefix = "/registry/bucket.example.com"

// BucketServerOptions contains state for master/api server
type BucketServerOptions struct {
	RecommendedOptions *genericoptions.RecommendedOptions
	// ComponentGlobalsRegistry is the registry where the effective versions and feature gates for all components are stored.
	ComponentGlobalsRegistry basecompatibility.ComponentGlobalsRegistry

	SharedInformerFactory informers.SharedInformerFactory
	StdOut                io.Writer
	StdErr                io.Writer

	AlternateDNS []string
}

func BucketVersionToKubeVersion(ver *version.Version) *version.Version {
	if ver.Major() != 1 {
		return nil
	}
	kubeVer := version.MustParse(baseversion.DefaultKubeBinaryVersion)
	// "1.2" maps to kubeVer
	offset := int(ver.Minor()) - 2
	mappedVer := kubeVer.OffsetMinor(offset)
	if mappedVer.GreaterThan(kubeVer) {
		return kubeVer
	}
	return mappedVer
}

// NewBucketServerOptions returns a new BucketServerOptions
func NewBucketServerOptions(out, errOut io.Writer) *BucketServerOptions {
	o := &BucketServerOptions{
		RecommendedOptions: genericoptions.NewRecommendedOptions(
			defaultEtcdPathPrefix,
			apiserver.Codecs.LegacyCodec(v1alpha1.SchemeGroupVersion),
		),
		ComponentGlobalsRegistry: compatibility.DefaultComponentGlobalsRegistry,

		StdOut: out,
		StdErr: errOut,
	}
	o.RecommendedOptions.Etcd.StorageConfig.EncodeVersioner = runtime.NewMultiGroupVersioner(v1alpha1.SchemeGroupVersion, schema.GroupKind{Group: v1alpha1.Group})
	return o
}

// NewCommandStartBucketServer provides a CLI handler for 'start master' command
// with a default BucketServerOptions.
func NewCommandStartBucketServer(ctx context.Context, defaults *BucketServerOptions, skipDefaultComponentGlobalsRegistrySet bool) *cobra.Command {
	o := *defaults
	cmd := &cobra.Command{
		Short: "Launch a Bucket API server",
		Long:  "Launch a Bucket API server",
		PersistentPreRunE: func(*cobra.Command, []string) error {
			if skipDefaultComponentGlobalsRegistrySet {
				return nil
			}
			return defaults.ComponentGlobalsRegistry.Set()
		},
		RunE: func(c *cobra.Command, args []string) error {
			if err := o.Complete(); err != nil {
				return err
			}
			if err := o.Validate(args); err != nil {
				return err
			}
			if err := o.RunBucketServer(c.Context()); err != nil {
				return err
			}
			return nil
		},
	}
	cmd.SetContext(ctx)

	flags := cmd.Flags()
	o.RecommendedOptions.AddFlags(flags)

	// The following lines demonstrate how to configure version compatibility and feature gates
	// for the "Bucket" component, as an example of KEP-4330.

	// Create an effective version object for the "Bucket" component.
	// This initializes the binary version, the emulation version and the minimum compatibility version.
	//
	// Note:
	// - The binary version represents the actual version of the running source code.
	// - The emulation version is the version whose capabilities are being emulated by the binary.
	// - The minimum compatibility version specifies the minimum version that the component remains compatible with.
	//
	// Refer to KEP-4330 for more details: https://github.com/kubernetes/enhancements/blob/master/keps/sig-architecture/4330-compatibility-versions
	defaultBucketVersion := "1.2"
	// Register the "Bucket" component with the global component registry,
	// associating it with its effective version and feature gate configuration.
	// Will skip if the component has been registered, like in the integration test.
	_, bucketFeatureGate := defaults.ComponentGlobalsRegistry.ComponentGlobalsOrRegister(
		apiserver.BucketComponentName, basecompatibility.NewEffectiveVersionFromString(defaultBucketVersion, "", ""),
		featuregate.NewVersionedFeatureGate(version.MustParse(defaultBucketVersion)))

	// Add versioned feature specifications for the "BanBucket" feature.
	// These specifications, together with the effective version, determine if the feature is enabled.
	utilruntime.Must(bucketFeatureGate.AddVersioned(map[featuregate.Feature]featuregate.VersionedSpecs{
		"BanBucket": {
			{Version: version.MustParse("1.0"), Default: false, PreRelease: featuregate.Alpha},
			{Version: version.MustParse("1.1"), Default: true, PreRelease: featuregate.Beta},
			{Version: version.MustParse("1.2"), Default: true, PreRelease: featuregate.GA, LockToDefault: true},
		},
	}))

	// Register the default kube component if not already present in the global registry.
	_, _ = defaults.ComponentGlobalsRegistry.ComponentGlobalsOrRegister(basecompatibility.DefaultKubeComponent,
		basecompatibility.NewEffectiveVersionFromString(baseversion.DefaultKubeBinaryVersion, "", ""), utilfeature.DefaultMutableFeatureGate)

	// Set the emulation version mapping from the "Bucket" component to the kube component.
	// This ensures that the emulation version of the latter is determined by the emulation version of the former.
	utilruntime.Must(defaults.ComponentGlobalsRegistry.SetEmulationVersionMapping(apiserver.BucketComponentName, basecompatibility.DefaultKubeComponent, BucketVersionToKubeVersion))

	defaults.ComponentGlobalsRegistry.AddFlags(flags)

	return cmd
}

// Validate validates BucketServerOptions
func (o BucketServerOptions) Validate(args []string) error {
	errors := []error{}
	errors = append(errors, o.RecommendedOptions.Validate()...)
	errors = append(errors, o.ComponentGlobalsRegistry.Validate()...)
	return utilerrors.NewAggregate(errors)
}

// Complete fills in fields required to have valid data
func (o *BucketServerOptions) Complete() error {
	return nil
}

// getOpenAPIDefinitionsWithFallback returns OpenAPI definitions with a fallback for external types
func getOpenAPIDefinitionsWithFallback(ref common.ReferenceCallback) map[string]common.OpenAPIDefinition {
	defs := sampleopenapi.GetOpenAPIDefinitions(ref)
	
	// Define stub definitions for external crossplane types that may be referenced
	// but don't have full OpenAPI definitions
	externalTypeStubs := []string{
		"github.com/crossplane/crossplane-runtime/v2/apis/common.Reference",
		"github.com/crossplane/crossplane-runtime/v2/apis/common.SecretReference",
		"github.com/crossplane/crossplane-runtime/v2/apis/common.Condition",
	}
	
	for _, typeName := range externalTypeStubs {
		if _, exists := defs[typeName]; !exists {
			defs[typeName] = common.OpenAPIDefinition{
				Schema: spec.Schema{
					SchemaProps: spec.SchemaProps{
						Type: []string{"object"},
						Properties: map[string]spec.Schema{
							"name":      {SchemaProps: spec.SchemaProps{Type: []string{"string"}}},
							"namespace": {SchemaProps: spec.SchemaProps{Type: []string{"string"}}},
						},
					},
				},
			}
		}
	}
	
	return defs
}

// Config returns config for the api server given BucketServerOptions
func (o *BucketServerOptions) Config() (*apiserver.Config, error) {
	// TODO have a "real" external address
	if err := o.RecommendedOptions.SecureServing.MaybeDefaultWithSelfSignedCerts("localhost", o.AlternateDNS, []net.IP{netutils.ParseIPSloppy("127.0.0.1")}); err != nil {
		return nil, fmt.Errorf("error creating self-signed certificates: %v", err)
	}

	serverConfig := genericapiserver.NewRecommendedConfig(apiserver.Codecs)

	serverConfig.OpenAPIConfig = genericapiserver.DefaultOpenAPIConfig(getOpenAPIDefinitionsWithFallback, openapi.NewDefinitionNamer(apiserver.Scheme))
	serverConfig.OpenAPIConfig.Info.Title = "Bucket"
	serverConfig.OpenAPIConfig.Info.Version = "0.1"

	serverConfig.OpenAPIV3Config = genericapiserver.DefaultOpenAPIV3Config(getOpenAPIDefinitionsWithFallback, openapi.NewDefinitionNamer(apiserver.Scheme))
	serverConfig.OpenAPIV3Config.Info.Title = "Bucket"
	serverConfig.OpenAPIV3Config.Info.Version = "0.1"

	serverConfig.FeatureGate = o.ComponentGlobalsRegistry.FeatureGateFor(basecompatibility.DefaultKubeComponent)
	serverConfig.EffectiveVersion = o.ComponentGlobalsRegistry.EffectiveVersionFor(apiserver.BucketComponentName)

	if err := o.RecommendedOptions.ApplyTo(serverConfig); err != nil {
		return nil, err
	}

	config := &apiserver.Config{
		GenericConfig: serverConfig,
		ExtraConfig:   apiserver.ExtraConfig{},
	}
	return config, nil
}

// RunBucketServer starts a new BucketServer given BucketServerOptions
func (o BucketServerOptions) RunBucketServer(ctx context.Context) error {
	config, err := o.Config()
	if err != nil {
		return err
	}

	server, err := config.Complete().New()
	if err != nil {
		return err
	}

	server.GenericAPIServer.AddPostStartHookOrDie("start-sample-server-informers", func(context genericapiserver.PostStartHookContext) error {
		config.GenericConfig.SharedInformerFactory.Start(context.Done())
		if o.SharedInformerFactory != nil {
			o.SharedInformerFactory.Start(context.Done())
		}
		return nil
	})

	return server.GenericAPIServer.PrepareRun().RunWithContext(ctx)
}
