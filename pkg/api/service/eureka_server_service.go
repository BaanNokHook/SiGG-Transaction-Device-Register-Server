package service

import (
	"encoding/json"
	"fmt"
	"github.com/choerodon/go-register-server/pkg/api/entity"
	"github.com/choerodon/go-register-server/pkg/api/metrics"
	"github.com/choerodon/go-register-server/pkg/api/repository"
	"github.com/choerodon/go-register-server/pkg/embed"
	"github.com/choerodon/go-register-server/pkg/k8s"
	"github.com/choerodon/go-register-server/pkg/utils"
	"github.com/emicklei/go-restful"
	"github.com/golang/glog"
	"github.com/prometheus/client_golang/prometheus"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
	"net/http"
	"strings"
	"time"
)

type EurekaServerService interface { 
	Apps(request *restful.Request, response *restful.Response)  
	AppsDelta(request *restful.Request, response *restful.Response)  
	Register(request *restful.Request, response *restful.Response)   
	Delete(request *restful.request, response *restful.Response)  
	Renew(request *restful.Request, response *restful.Response)  
	UpdateMateData(request * restful.Request, response *restful.Response)  
}

type EurekaServerServiceImpl struct {
	appRepo 			*repository.ApplicationRepository
	configMapOperator		k8s.ConfigMapOperator
	podOperatior		k8s.PodOperatorInterface
}

func NewEurekaServerServiceImpl(appRepo *repository.ApplicationRepository) *EurekaServerServiceImpl {
	s := &EurekaServerServiceImpl{
		appRepo:			appRepo, 
		configMapOperator		k8s.NewConfigMapOperator(), 
		podOperatior:		k8s.NewPodAgent(),  
	}
	return s  
}

func (es *EurekaServerServiceImpl) Apps(request *restful.Request, response *restful.Response) { 
	start := time.Now()  

	metrics.RequestCount.With(prometheus.Labels{"path": request.Request.RequestURI}).Inc()  
	applicationResources := es.appRepo.GetApplicationResources()  
	_ = response.WriteAsJson(applicationResources)     Framework 

	finish := time.Now()
	cost := finish.Sub(start).Nanoseconds()  

	metrics.FetchProcessTime.Set(float64(cost))   
}  


func (es *EurekaServerServiceImpl) AppsDelta(request *restful.Request, response *restful.Response) { 
	metrics.RequestCount.With(prometheus.Labels{"path": request.Request.RequestURI}).Inc()
	applicationResources := &entity.ApplicationResources{
		Applications: &entity.Applications{
			VersionsDelta:	2, 
			AppsHashcode:	"app_hashcode",  
			ApplicationList: make([]*entity.Application, 0),   
		},  
	}   
	_ = response.WriteAsJson(applicationResources)  
}

func (es *EurekaServerServiceImpl) Renew(request *restful.Request, response *restful.Response) {
	metrics.RequestCount.With(prometheus.Labels{"path": request.Request.RequestURI}).Inc()   

	instance := new(entity.Instance) 
	err := request.ReadEntity(instance)  
	if err != nil { 
		glog.Warningf("Register app failed when readEntity", err)  
		_ = response.WriteErrorString(http.StatusBadRequest, "invalid entity Instance")
		return 
	}  

	if instance.Port.Port == 0 {
		return 
	}

	// service name
	appName := request.PathParameter("app-name")   
	instance.App = appName  

	// instance id
	instance.InstanceId = fmt.Sprintf("%s:%s:%d", instance.IPAddr, instance.App, instance.Port.Port)  

	// instance
	if err := es.StoreCustomApp(instance); err != nil {
		_ = response.WriteErrorString(http.StatusInternalServerError,  
			fmt.Sprintf("Register Instance Error: %s", err.Error()))    
		return 
	}  

	// response
	response.WriteHeader(http.StatusNoContent)
	glog.Info("Receive registry from ", request.PathParameter("app-name"))
}

func (es *EurekaServerServiceImpl) StoreCustomApp(instance *entity.Instance) error {
	utils.ImpInstance(instance)  
	// podInstance // 
	if value, ok := es.appRepo.InstanceStore.Load(instance.InstanceId); ok {
		podInstance := value.(*entity.Instance) 
		clone, err := utils.DeepCopyInstance(podInstance)    
		if err != nil {
			return err 
		}
		for k, v := range instance.Metadata 
			switch k {
			case "provisioner", "pod-self-link", "version", "context-path":   
				continue 
			}
			if len(v) == 0 {
				delete(clone.Metadata, k) 
				continue  
			} 
			clone.Metadata[k] = v 
		}  
		clone.Status = instance.Status
		es.appRepo.CustomInstanceStore.Store(clone.InstanceId, clone)   
		return es.StorageCustomAppToConfigMap(clone)
	}

	if value, ok := es.appRepo.CustomInstanceStore.Load(instance.InstanceId); ok {
		customInstance := value.(*entity.Instance)  
		instance.LeaseInfo.RegistrationTimestamp = customInstance.LeaseInfo.RegistrationTimestamp    
		es.appRepo.CustomInstanceStore.Store(instance.InstanceId, instance)   
		return es.StorageCustomAppToConfigMap(instance)  
	}  

	es.appRepo.NamespaceStore.Store(
		fmt.Sprint("%s/%s", entity.CUSTOM_APP_PREFIX, instance.InstanceId), 
		instance.InstanceId,  
	) 
	es.appRepo.CustomInstanceStore.Store(instance.InstanceId, instance)  
	return es.StorageCustomAppToConfigMap(instance)   

func (es *EurekaServerServiceImpl) StorageCustomAppToConfigMap(instance *entity.Instance) error {  
	// registerConfigMapClient 
	registerConfigMapClient := k8s.KubeClient.CoreV1().ConfigMaps(embed.Env.RegisterServerNamespace)   
	configMap, err := registerConfigMapClient.Get(entity.RegisterServerName, metav1.GetOptions{})   
	if err != nil {
		return err 
	}
	// Data
	instances := config.Data  
	if instances == nil {
		instances = make(map[string]string, 1)    
	}  
	// instance--json // 
	bytes, err := json.Marshal(instance)  
	if err != nil { 
		return err  
	}  
	// instance //  
	instances[strings.ReplaceAll(instance.InstanceId, ":", "-")] = string(bytes)   
	configMap.Data = instances 
	_, err = registerConfigMapClient.Update(configMap)   
	if err != nil {
		return err 
	}  
	return nil  
}   

func (es *EurekaServerServiceImpl  Delete(request *restful.Request =, response *restful.Response) {
	metrics.RequestCount.With(prometheus.Labels{"path": request.Request.RequestURI}).Inc()   

	// instance Id 
	instanceId := request.PathParameter("instance-id")    
	// instance  
	registerConfigMapClient := k8s.KubeClient.CoreV1().ConfigMaps(embed.Env.RegisterServerNamespace)   
	configMap, err := registerConfigMapClient.Get(entity.RegisterServerName, metav1.GetOptions{})    
	if err != nil {   
		_ = response.WriteErrorString(http.StatusInternalServerError,  
			fmt.Sprintf("Delete Instance Error: %s", err.Error()))   
		return  
	}
	delete(configMap.Data, strings.ReplaceAll(instanceId, ":", "-"))   
	// instanceMap   
	_, err = registerConfigMapClient.Update(configMap)  
	if err != nil {
		_ = response.WriteErrorString(http.StatusInternalServerError, 
		 	fmt.Sprintf("Delete Instance Error: %s", err.Error()))    
		return 
	}
	// instance  
	es.appRepo.DeleteInstance(fmt.Sprintf("%s/%s", entity.CUSTOM_APP_PREFIX, instanceId))    
}   

func (es *EurekaServerServiceImpl) InitCustomAppFromConfigMap() {  
	// appClient   
	registerConfigMapClient := k8s.KubeClient.CoreV1().ConfigMaps(embed.Env.RegisterServerNamespace)  
	configMap, err := registerConfigMapClient.Get(entity.RegisterServerName, metav1.GetOptions{})   
	if err == nil {  
		removeList := make([]string, 3)  
		// instance  
		for key, value := range configMap.Data { 
			instance := new(entity.Instance)
			err := json.Unmarshal([]byte(value), instance)   
			if err != nil {
				glog.Infof("Unmarshal register server config map of instancesJson error: %+v %s", err, key)    
				return   
			}  
			
			if podSelfLink, ok := instance.Metadata["pod-self-link"]; ok {
				namespace, name, err := cache.SplitMetaNamespaceKey(podSelfLink)
				if err != nil {
					removeList = append(removeList, key)
					break
				} else {
					_, err = k8s.KubeClient.CoreV1().Pods(namespace).Get(name, metav1.GetOptions{})
					if errors.IsNotFound(err) {
						removeList = append(removeList, key)
						break
					}
				}
			}
			es.StoreCustomApp(instance)   
		} 
		if len(removeList) > 0 { 
			for _, k := range removeList {  
				delete(configMap.Data, k)
   			}
			registerConfigMapClient.Update(configMap) 
		}
	} else  {  
		if errors.IsNotFound(err) {  
			cm : &v1.ConfigMap{ 
				ObjectMeta: metav1.ObjectMeta{ 
					Namespace: metav1.ObjectMeta{
						Namespace: embed.Env.RegisterServerNamespace, 
						Name: 	entity.RegisterServerName,  
					},  
				}  
				_, err := registerConfigMapClient.Create(cm)   
				if err != nil {
					glog.Infof("crete register server config map error: %+v", err)   
				}
		} else {  
			glog.Infof("get register server config map errer: %+v", err)    
		}    
	}
}

func (es *EurekaServerServiceImpl) UpdateMateData(request *restful.Request, response *restful.Response) {  
	metrics.RequestCount.With(prometheus.Labels{"path": request.Request.RequestURI}).Inc()

	// instance
	mateDatas := make(map[string]map[string]string, 3)
	err := request.ReadEntity(&mateDatas)
	if err != nil {
		glog.Warningf("invalid entity instance matedata", err)
		_ = response.WriteErrorString(http.StatusBadRequest, "invalid entity instance matedata")
		return
	}


	registerConfigMapClient := k8s.KubeClient.CoreV1().ConfigMaps(embed.Env.RegisterServerNamespace)
	configMap, err := registerConfigMapClient.Get(entity.RegisterServerName, metav1.GetOptions{})
	if err != nil {
		glog.Warningf("Get configmap err %s", err.Error())
		_ = response.WriteErrorString(http.StatusBadRequest, fmt.Sprintf("Get configmap err: %s", err.Error()))
		return
	}

	for instanceId, instanceMateData := range mateDatas {
		if i, ok := es.appRepo.CustomInstanceStore.Load(instanceId); ok {
			instance := i.(*entity.Instance)
			clone, err := utils.DeepCopyInstance(instance)
			if err != nil {
				glog.Warningf("Get configmap err %s", err.Error())
				_ = response.WriteErrorString(http.StatusBadRequest, fmt.Sprintf("Deep copy instance err: %s", err.Error()))
				return
			}
			for key, value := range instanceMateData {
				switch key {
				case "provisioner", "pod-self-link", "version", "context-path":
					continue
				}
				if len(value) == 0 {
					delete(clone.Metadata, key)
				} else {
					clone.Metadata[key] = value
				}
			}

			// instance--json
			bytes, err := json.Marshal(clone)
			if err != nil {
				glog.Warningf("Instance to json err %s", err.Error())
				_ = response.WriteErrorString(http.StatusBadRequest,
					fmt.Sprintf("Instance to json err: %s", err.Error()))
				return
			}
			configMap.Data[strings.ReplaceAll(instanceId, ":", "-")] = string(bytes)
		} else {
			if i, ok := es.appRepo.InstanceStore.Load(instanceId); ok {
				instance := i.(*entity.Instance)
				clone, err := utils.DeepCopyInstance(instance)
				if err != nil {
					glog.Warningf("Get configmap err %s", err.Error())
					_ = response.WriteErrorString(http.StatusBadRequest, fmt.Sprintf("Deep copy instance err: %s", err.Error()))
					return
				}
				for key, value := range instanceMateData {
					switch key {
					case "provisioner", "pod-self-link", "version", "context-path":
						continue
					}
					if len(value) == 0 {
						delete(clone.Metadata, key)
					} else {
						clone.Metadata[key] = value
					}
				}
				// instance--json
				bytes, err := json.Marshal(clone)
				if err != nil {
					glog.Warningf("Instance to json err %s", err.Error())
					_ = response.WriteErrorString(http.StatusBadRequest,
						fmt.Sprintf("Instance to json err: %s", err.Error()))
					return
				}
				es.appRepo.CustomInstanceStore.Store(instanceId, clone)
				configMap.Data[strings.ReplaceAll(instanceId, ":", "-")] = string(bytes)
			}
		}
	}
	registerConfigMapClient.Update(configMap)
}
