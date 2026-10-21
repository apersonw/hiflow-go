// 目录接口服务端骨架示例：用内存目录（JSON 数据文件）提供 HiFlow 组织架构目录接口。
// 实际对接时把 iamserver.Static 换成查询自己 IAM 数据的实现。
//
//	go run ./examples/iamserver -data ../../tools/iammock/org.example.json -id hfdir_demo -secret demo-secret
package main

import (
	"flag"
	"log"
	"net/http"
	"strings"

	"github.com/apersonw/hiflow-go/iamserver"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8096", "监听地址")
	base := flag.String("base", "/hiflow/v1", "基础地址的路径前缀")
	data := flag.String("data", "", "组织数据 JSON 文件")
	id := flag.String("id", "", "目录 ID（HiFlow 管理后台生成）")
	secret := flag.String("secret", "", "目录密钥")
	flag.Parse()
	d, err := iamserver.LoadStaticFile(*data)
	if err != nil {
		log.Fatal(err)
	}
	h := iamserver.New(iamserver.NewStatic(d), iamserver.Options{
		DirectoryID: *id, Secret: *secret, Provider: "hiflow-go iamserver example",
		RecursiveMembers: true, RoleDepartmentScope: true,
		OnError: func(r *http.Request, err error) { log.Printf("%s %s: %v", r.Method, r.URL.Path, err) },
	})
	prefix := strings.TrimRight(*base, "/")
	http.Handle(prefix+"/", http.StripPrefix(prefix, h))
	log.Printf("目录接口：http://%s%s", *addr, prefix)
	log.Fatal(http.ListenAndServe(*addr, nil))
}
