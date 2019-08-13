package kube_event

import (
	"fmt"

	"github.com/flant/addon-operator/pkg/module_manager"
	"github.com/flant/addon-operator/pkg/task"
	"github.com/flant/shell-operator/pkg/hook/kube_event"
	"github.com/flant/shell-operator/pkg/kube_events_manager"

	"github.com/romana/rlog"
)

// MakeKubeEventHookDescriptors converts hook config into KubeEventHook structures
func MakeKubeEventHookDescriptors(hook module_manager.Hook, hookConfig *module_manager.HookConfig) []*kube_event.KubeEventHook {
	res := make([]*kube_event.KubeEventHook, 0)

	for _, config := range hookConfig.OnKubernetesEvent {
		if config.NamespaceSelector.Any {
			res = append(res, ConvertOnKubernetesEventToKubeEventHook(hook, config, ""))
		} else {
			for _, namespace := range config.NamespaceSelector.MatchNames {
				res = append(res, ConvertOnKubernetesEventToKubeEventHook(hook, config, namespace))
			}
		}
	}

	return res
}

func ConvertOnKubernetesEventToKubeEventHook(hook module_manager.Hook, config kube_events_manager.OnKubernetesEventConfig, namespace string) *kube_event.KubeEventHook {
	return &kube_event.KubeEventHook{
		HookName:     hook.GetName(),
		Name:         config.Name,
		EventTypes:   config.EventTypes,
		Kind:         config.Kind,
		Namespace:    namespace,
		Selector:     config.Selector,
		JqFilter:     config.JqFilter,
		AllowFailure: config.AllowFailure,
		Debug:        !config.DisableDebug,
	}
}

type KubeEventsHooksController interface {
	EnableGlobalHooks(moduleManager module_manager.ModuleManager, eventsManager kube_events_manager.KubeEventsManager) error
	EnableModuleHooks(moduleName string, moduleManager module_manager.ModuleManager, eventsManager kube_events_manager.KubeEventsManager) error
	DisableModuleHooks(moduleName string, moduleManager module_manager.ModuleManager, eventsManager kube_events_manager.KubeEventsManager) error
	HandleEvent(kubeEvent kube_events_manager.KubeEvent) (*struct{ Tasks []task.Task }, error)
}

type MainKubeEventsHooksController struct {
	GlobalHooks    map[string]*kube_event.KubeEventHook
	ModuleHooks    map[string]*kube_event.KubeEventHook
	EnabledModules []string
}

// NewMainKubeEventsHooksController returns new instance of MainKubeEventsHooksController
func NewMainKubeEventsHooksController() *MainKubeEventsHooksController {
	obj := &MainKubeEventsHooksController{}
	obj.GlobalHooks = make(map[string]*kube_event.KubeEventHook)
	obj.ModuleHooks = make(map[string]*kube_event.KubeEventHook)
	obj.EnabledModules = make([]string, 0)
	return obj
}

// EnableGlobalHooks starts kube events informers for all global hooks
func (obj *MainKubeEventsHooksController) EnableGlobalHooks(moduleManager module_manager.ModuleManager, eventsManager kube_events_manager.KubeEventsManager) error {
	globalHooks := moduleManager.GetGlobalHooksInOrder(module_manager.KubeEvents)

	for _, globalHookName := range globalHooks {
		globalHook, _ := moduleManager.GetGlobalHook(globalHookName)

		for _, desc := range MakeKubeEventHookDescriptors(globalHook, &globalHook.Config.HookConfig) {
			configId, err := eventsManager.Run(desc.EventTypes, desc.Kind, desc.Namespace, desc.Selector, desc.ObjectName, desc.JqFilter, desc.Debug)
			if err != nil {
				return err
			}
			obj.GlobalHooks[configId] = desc

			rlog.Debugf("MAIN: run informer %s for global hook %s", configId, globalHook.Name)
		}
	}

	return nil
}

// EnableModuleHooks starts kube events informers for all module hooks
func (obj *MainKubeEventsHooksController) EnableModuleHooks(moduleName string, moduleManager module_manager.ModuleManager, eventsManager kube_events_manager.KubeEventsManager) error {
	for _, enabledModuleName := range obj.EnabledModules {
		if enabledModuleName == moduleName {
			// already enabled
			return nil
		}
	}

	moduleHooks, err := moduleManager.GetModuleHooksInOrder(moduleName, module_manager.KubeEvents)
	if err != nil {
		return err
	}

	for _, moduleHookName := range moduleHooks {
		moduleHook, _ := moduleManager.GetModuleHook(moduleHookName)

		for _, desc := range MakeKubeEventHookDescriptors(moduleHook, &moduleHook.Config.HookConfig) {
			configId, err := eventsManager.Run(desc.EventTypes, desc.Kind, desc.Namespace, desc.Selector, desc.ObjectName, desc.JqFilter, desc.Debug)
			if err != nil {
				return err
			}
			obj.ModuleHooks[configId] = desc

			rlog.Debugf("MAIN: run informer %s for module hook %s", configId, moduleHook.Name)
		}
	}

	obj.EnabledModules = append(obj.EnabledModules, moduleName)

	return nil
}

// DisableModuleHooks stops informers for module hooks
func (obj *MainKubeEventsHooksController) DisableModuleHooks(moduleName string, moduleManager module_manager.ModuleManager, eventsManager kube_events_manager.KubeEventsManager) error {
	moduleEnabledInd := -1
	for i, enabledModuleName := range obj.EnabledModules {
		if enabledModuleName == moduleName {
			moduleEnabledInd = i
			break
		}
	}
	if moduleEnabledInd < 0 {
		return nil
	}
	obj.EnabledModules = append(obj.EnabledModules[:moduleEnabledInd], obj.EnabledModules[moduleEnabledInd+1:]...)

	disabledModuleHooks, err := moduleManager.GetModuleHooksInOrder(moduleName, module_manager.KubeEvents)
	if err != nil {
		return err
	}

	for configId, desc := range obj.ModuleHooks {
		for _, disabledModuleHookName := range disabledModuleHooks {
			if desc.HookName == disabledModuleHookName {
				err := eventsManager.Stop(configId)
				if err != nil {
					return err
				}

				delete(obj.ModuleHooks, configId)

				break
			}
		}
	}

	return nil
}

// HandleEvent creates a task from kube event
func (obj *MainKubeEventsHooksController) HandleEvent(kubeEvent kube_events_manager.KubeEvent) (*struct{ Tasks []task.Task }, error) {
	res := &struct{ Tasks []task.Task }{Tasks: make([]task.Task, 0)}
	var desc *kube_event.KubeEventHook
	var taskType task.TaskType

	if moduleDesc, hasKey := obj.ModuleHooks[kubeEvent.ConfigId]; hasKey {
		desc = moduleDesc
		taskType = task.ModuleHookRun
	} else if globalDesc, hasKey := obj.GlobalHooks[kubeEvent.ConfigId]; hasKey {
		desc = globalDesc
		taskType = task.GlobalHookRun
	}

	if desc != nil && taskType != "" {
		bindingName := desc.Name
		if desc.Name == "" {
			bindingName = module_manager.ContextBindingType[module_manager.KubeEvents]
		}

		bindingContext := make([]module_manager.BindingContext, 0)
		for _, kEvent := range kubeEvent.Events {
			bindingContext = append(bindingContext, module_manager.BindingContext{
				Binding:           bindingName,
				ResourceEvent:     kEvent,
				ResourceNamespace: kubeEvent.Namespace,
				ResourceKind:      kubeEvent.Kind,
				ResourceName:      kubeEvent.Name,
			})
		}

		newTask := task.NewTask(taskType, desc.HookName).
			WithBinding(module_manager.KubeEvents).
			WithBindingContext(bindingContext).
			WithAllowFailure(desc.Config.AllowFailure)

		res.Tasks = append(res.Tasks, newTask)
	} else {
		return nil, fmt.Errorf("Unknown kube event: no such config id '%s' registered", kubeEvent.ConfigId)
	}

	return res, nil
}
