/*
Copyright 2017 The Kubernetes Authors.

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

package bucket

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/apiserver/pkg/registry/generic"
	"k8s.io/apiserver/pkg/storage"
	"k8s.io/apiserver/pkg/storage/names"
	"k8s.io/sample-apiserver/pkg/apis/providerceph/v1alpha1"
)

// NewStrategy creates and returns a bucketStrategy instance
func NewStrategy(typer runtime.ObjectTyper) bucketStrategy {
	return bucketStrategy{typer, names.SimpleNameGenerator}
}

// GetAttrs returns labels.Set, fields.Set, and error in case the given runtime.Object is not a Bucket
func GetAttrs(obj runtime.Object) (labels.Set, fields.Set, error) {
	apiserver, ok := obj.(*v1alpha1.Bucket)
	if !ok {
		return nil, nil, fmt.Errorf("given object is not a Bucket")
	}
	return labels.Set(apiserver.ObjectMeta.Labels), SelectableFields(apiserver), nil
}

// MatchBucket is the filter used by the generic etcd backend to watch events
// from etcd to clients of the apiserver only interested in specific labels/fields.
func MatchBucket(label labels.Selector, field fields.Selector) storage.SelectionPredicate {
	return storage.SelectionPredicate{
		Label:    label,
		Field:    field,
		GetAttrs: GetAttrs,
	}
}

// SelectableFields returns a field set that represents the object.
func SelectableFields(obj *v1alpha1.Bucket) fields.Set {
	return generic.ObjectMetaFieldsSet(&obj.ObjectMeta, true)
}

type bucketStrategy struct {
	runtime.ObjectTyper
	names.NameGenerator
}

func (bucketStrategy) NamespaceScoped() bool {
	return false
}

func (bucketStrategy) PrepareForCreate(ctx context.Context, obj runtime.Object) {
}

func (bucketStrategy) PrepareForUpdate(ctx context.Context, obj, old runtime.Object) {
}

func (bucketStrategy) Validate(ctx context.Context, obj runtime.Object) field.ErrorList {
	return nil
}

// WarningsOnCreate returns warnings for the creation of the given object.
func (bucketStrategy) WarningsOnCreate(ctx context.Context, obj runtime.Object) []string { return nil }

func (bucketStrategy) AllowCreateOnUpdate() bool {
	return false
}

func (bucketStrategy) AllowUnconditionalUpdate() bool {
	return false
}

func (bucketStrategy) Canonicalize(obj runtime.Object) {
}

func (bucketStrategy) ValidateUpdate(ctx context.Context, obj, old runtime.Object) field.ErrorList {
	return field.ErrorList{}
}

// WarningsOnUpdate returns warnings for the given update.
func (bucketStrategy) WarningsOnUpdate(ctx context.Context, obj, old runtime.Object) []string {
	return nil
}
