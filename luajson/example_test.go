package luajson_test

import (
	"context"
	"fmt"

	lua "github.com/folbricht/glua"
	"github.com/folbricht/glua/luajson"
)

func Example() {
	rt := lua.MustNew(luajson.Module("json"))
	script, err := rt.Load(context.Background(), "hook.lua", `
		function handle(body)
			local req = json.decode(body)
			return json.encode({user = req.user, items = #req.items, ok = true})
		end`)
	if err != nil {
		panic(err)
	}
	handle, _ := script.Func1[string, string]("handle")
	out, err := handle(context.Background(), `{"user": "ann", "items": [1, 2, 3]}`)
	fmt.Println(out, err)

	v, _ := luajson.Unmarshal([]byte(`{"port": 8080}`))
	b, _ := luajson.Marshal(v)
	fmt.Println(string(b))
	// Output:
	// {"items":3,"ok":true,"user":"ann"} <nil>
	// {"port":8080}
}
