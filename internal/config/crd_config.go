package config

import (
	"context"
	"errors"
	"fmt"

	"github.com/meshery/meshery-operator/pkg/client"
	"github.com/meshery/meshkit/utils"
	"golang.org/x/exp/slices"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
)

var (
	namespace = "meshery"          // Namespace for the Custom Resource
	crName    = "meshery-meshsync" // Name of the custom resource
	version   = "v1alpha1"         // Version of the Custom Resource
	group     = "meshery.io"       //Group for the Custom Resource
	resource  = "meshsyncs"        //Name of the Resource
)

func GetMeshsyncCRDConfigs(dyClient dynamic.Interface) (*MeshsyncConfig, error) {
	// make a call to get the custom resource
	crd, err := GetMeshsyncCRD(dyClient)

	if err != nil {
		return nil, ErrInitConfig(err)
	}

	if crd == nil {
		return nil, ErrInitConfig(errors.New("Custom Resource is nil"))
	}

	spec := crd.Object["spec"]
	specMap, ok := spec.(map[string]interface{})
	if !ok {
		return nil, ErrInitConfig(errors.New("Unable to convert spec to map"))
	}
	configObj := specMap["watch-list"]
	if configObj == nil {
		return nil, ErrInitConfig(errors.New("Custom Resource does not have Meshsync Configs"))
	}
	configStr, err := utils.Marshal(configObj)
	if err != nil {
		return nil, ErrInitConfig(err)
	}

	configMap := corev1.ConfigMap{}
	err = utils.Unmarshal(string(configStr), &configMap)

	if err != nil {
		return nil, ErrInitConfig(err)
	}

	// populate the required configs
	meshsyncConfig, err := PopulateConfigs(configMap)

	if err != nil {
		return nil, ErrInitConfig(err)
	}
	return meshsyncConfig, nil
}

func GetMeshsyncCRD(dyClient dynamic.Interface) (*unstructured.Unstructured, error) {
	// initialize the group version resource to access the custom resource
	gvr := schema.GroupVersionResource{Version: version, Group: group, Resource: resource}
	return dyClient.Resource(gvr).Namespace(namespace).Get(context.TODO(), crName, metav1.GetOptions{})
}

func GetMeshsyncCRDConfigsLocal() (*MeshsyncConfig, error) {
	// populate the required configs
	meshsyncConfig, err := PopulateConfigsFromMap(LocalMeshsyncConfig)

	if err != nil {
		// // this hides actual error message
		// return nil, ErrInitConfig(err)
		return nil, err
	}
	return meshsyncConfig, nil
}

// PopulateConfigs compares the default configs and the whitelist and blacklist
func PopulateConfigs(configMap corev1.ConfigMap) (*MeshsyncConfig, error) {
	return PopulateConfigsFromMap(configMap.Data)
}

func PopulateConfigsFromMap(data map[string]string) (*MeshsyncConfig, error) {
	meshsyncConfig := &MeshsyncConfig{}

	if _, ok := data["blacklist"]; ok {
		if len(data["blacklist"]) > 0 {
			err := utils.Unmarshal(data["blacklist"], &meshsyncConfig.BlackList)
			if err != nil {
				return nil, ErrInitConfig(err)
			}
		}
	}

	if _, ok := data["whitelist"]; ok {
		if len(data["whitelist"]) > 0 {
			err := utils.Unmarshal(data["whitelist"], &meshsyncConfig.WhiteList)
			if err != nil {
				return nil, ErrInitConfig(err)
			}
		}
	}

	// ensure that atleast one of whitelist or blacklist has been supplied
	if len(meshsyncConfig.BlackList) == 0 && len(meshsyncConfig.WhiteList) == 0 {
		return nil, ErrInitConfig(errors.New("Both whitelisted and blacklisted resources missing"))
	}

	// ensure that only one of whitelist or blacklist has been supplied
	if len(meshsyncConfig.BlackList) != 0 && len(meshsyncConfig.WhiteList) != 0 {
		return nil, ErrInitConfig(errors.New("Both whitelisted and blacklisted resources not currently supported"))
	}

	// Handle global resources
	globalPipelines := make(PipelineConfigs, 0)
	localPipelines := make(PipelineConfigs, 0)

	if len(meshsyncConfig.WhiteList) != 0 {
		for _, v := range Pipelines[GlobalResourceKey] {
			if idx := slices.IndexFunc(meshsyncConfig.WhiteList, func(c ResourceConfig) bool { return c.Resource == v.Name }); idx != -1 {
				config := meshsyncConfig.WhiteList[idx]
				v.Events = config.Events
				globalPipelines = append(globalPipelines, v)
			}
		}
		if len(globalPipelines) > 0 {
			meshsyncConfig.Pipelines = map[string]PipelineConfigs{}
			meshsyncConfig.Pipelines[GlobalResourceKey] = globalPipelines
		}

		// Handle local resources
		for _, v := range Pipelines[LocalResourceKey] {
			if idx := slices.IndexFunc(meshsyncConfig.WhiteList, func(c ResourceConfig) bool { return c.Resource == v.Name }); idx != -1 {
				config := meshsyncConfig.WhiteList[idx]
				v.Events = config.Events
				localPipelines = append(localPipelines, v)
			}
		}

		if len(localPipelines) > 0 {
			if meshsyncConfig.Pipelines == nil {
				meshsyncConfig.Pipelines = make(map[string]PipelineConfigs)
			}
			meshsyncConfig.Pipelines[LocalResourceKey] = localPipelines
		}

	} else {

		for _, v := range Pipelines[GlobalResourceKey] {
			if idx := slices.IndexFunc(meshsyncConfig.BlackList, func(c string) bool { return c == v.Name }); idx == -1 {
				v.Events = DefaultEvents
				globalPipelines = append(globalPipelines, v)
			}
		}
		if len(globalPipelines) > 0 {
			meshsyncConfig.Pipelines = map[string]PipelineConfigs{}
			meshsyncConfig.Pipelines[GlobalResourceKey] = globalPipelines
		}

		// Handle local resources
		for _, v := range Pipelines[LocalResourceKey] {
			if idx := slices.IndexFunc(meshsyncConfig.BlackList, func(c string) bool { return c == v.Name }); idx == -1 {
				v.Events = DefaultEvents
				localPipelines = append(localPipelines, v)
			}
		}

		if len(localPipelines) > 0 {
			if meshsyncConfig.Pipelines == nil {
				meshsyncConfig.Pipelines = make(map[string]PipelineConfigs)
			}
			meshsyncConfig.Pipelines[LocalResourceKey] = localPipelines
		}
	}

	return meshsyncConfig, nil
}

func PatchCRVersion(config *rest.Config) error {
	meshsyncClient, err := client.New(config)
	if err != nil {
		return ErrInitConfig(fmt.Errorf("unable to update MeshSync configuration"))
	}

	patchedResource := map[string]interface{}{
		"spec": map[string]interface{}{
			"version": Server["version"],
		},
	}
	byt, err := utils.Marshal(patchedResource)
	if err != nil {
		return ErrInitConfig(fmt.Errorf("unable to update MeshSync configuration"))
	}
	_, err = meshsyncClient.CoreV1Alpha1().MeshSyncs("meshery").Patch(context.TODO(), crName, types.MergePatchType, []byte(byt), metav1.PatchOptions{})
	if err != nil {
		return ErrInitConfig(fmt.Errorf("unable to update MeshSync configuration"))
	}
	return nil
}
