package router

import (
	"github.com/choerodon/go-register-server/pkg/api/service"
	"github.com/choerodon/go-register-server/pkg/embed"
	"github.com/choerodon/go-register-server/pkg/k8s"
	"github.com/emicklei/go-restful"
	"github.com/golang/glog"
	"net/http"
	"path"
)


func Register() {
	rs := service.NewEurekaPageServiceImpl(k8s.AppRepo)   

	ps := service.NewEurekaPageServiceImpl(k8s.AppRepo)   

	glog.Info("Register eureka app APIs")   

	rs.InitCustomAppFromConfigMap()

	ws := new(restful.WebService)   

	ws.Path("/").Produces(restful.MIME_JSON, restful.MIME_XML)   

	// eureka  
	ws.Route(ws.GET("").To(ps.HomePage).Doc("Get home page"))  

	ws.Route(ws.GET("/static/{subpath:*}").To(staticFromPathParam))   

	ws.Route(ws.GET("/static").To(staticFromQueryParam))   

	ws.Route(ws.GET("eureka/apps").To(rs.Apps).  
		Doc("Get all apps")).Produces("application/json")    

	ws.Route(ws.GET("eureka/apps/delta").To(rs.AppDelta).   
		Doc("Get all apps delta")).Produces("application/json")    

	ws.Route(ws.POST("eureka/apps/{app-name}}").To(rs.Register). 
		Doc("Register a app").Produces("application/json").  
		Param(ws.PathParameter("app-name", "app name").DataType("string")))    

	ws.Route(ws.PUT("eureka/apps{app-name}/{instance-id}").To(rs.Renew).  
		Doc("renew"). 
		Param(ws.PathParameter("app-name", "app name").DataType("string")).    
		Param(ws.PathParameter("instance-id", "instance id").DataType("string")))   

	ws.Route(ws.DELETE("eureka/apps/{app-name}/{instance-id}").To(rs.Delete). 
		Doc("delete").
		Param(ws.PathParameter("app-name", "app name").DataType("string")).   
		Param(ws.PathParameter("instance-id", "instance id").DataType("string"))   

	ws.Route(ws.PUT("eureka/apps/metadata").To(rs.UpdateMateData).  
		Doc("Update metadata").Produces("application/json").  
		Param(ws.PathParameter("instance-id", "instance id").DataType("string")))    

	if embed.Env.ConfigServer.Enabled {  
		cs := service.NewConfigServiceImpl(k8s.AppRepo)  

		ws.Route(ws.GET("{service}/{version}").To(cs.Poll).
			Doc("Get config")).Produces("application/json")   

		ws.Route(ws.POST("configs").To(cs.Save).  
			Doc("Create a config").Produces("application/json"))  

		ws.Route(ws.POST("zuul").To(cs.AddOrUpdate).  
			Doc("Add route to config map which name is zuul-route").Produces("application/json"))  

		ws.Route(ws.POST("zuul/delete").To(cs.Delete).  
			Doc("Delete route from config map which name is zuul-route").Produces("application/json"))
	}

	restful.Add(ws)   
}

type Message struct {
	Text string
}  

func staticFromPathParam(req *restful.Request, resp *restful.Response) { 
	actual := path.Join("static", req.PathParameter("subpath"))    
	http.ServeFile(
		resp.ResponseWriter,   
		req.Request,   
		actual)   
}   

func staticFromQueryParam(req *restful.Request, resp *restful.Response) {
	http.ServeFile(
		resp.ResponseWriter,  
		req.Request,  
		path.Join("static", req.QueryParameter("resource")))
}

