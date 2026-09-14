from bottle import response, route, run
import msgspec
from parser import parse_json  # 假设你把之前的代码存为了 parser.py

@route('/')
def index():
    return "<h1>server running...</h1>"

@route('/api/test')
def api_test():
    # Bottle 会自动将字典转换为 JSON 并设置 application/json 响应头
    return {"message": "Bottle 服务器运行正常！", "status": "success"}

# 去掉 'GET'，因为 parse_json 包含了文件写入等修改状态的操作
@route('/api/save', method=['POST'])
def api_save():
    try:
        result = parse_json()
        return {
            "status": "success",
            "message": "解析并保存成功",
            "data": result
        }
    except FileNotFoundError as e:
        response.status = 404
        return {"status": "error", "message": str(e)}
    except (msgspec.DecodeError, msgspec.ValidationError) as e:
        response.status = 400
        return {"status": "error", "message": f"JSON 格式或结构有误: {e}"}
    except Exception as e:
        response.status = 500
        return {"status": "error", "message": str(e)}

def main():
    print("服务器正在启动，请访问: http://127.0.0.1:8033")
    # 提示：在部署到生产环境时，建议将 debug 和 reloader 都设置为 False
    run(host='127.0.0.1', server='waitress', port=8033, debug=True, reloader=True)

if __name__ == "__main__":
    main()