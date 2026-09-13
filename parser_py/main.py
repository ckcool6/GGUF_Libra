import json

from bottle import response, route, run
import msgspec
from parser import parse_json 


@route('/')
def index():
    return "<h1>server running...</h1>"

@route('/api/test')
def api_test():
    return {"message": "Bottle 服务器运行正常！", "status": "success"}

@route('/api/save', method=['GET', 'POST'])
def api_save():
    try:
        result = parse_json()
        return {
            "status": "success",
            "message": "解析成功",
            "data": result
        }
    except FileNotFoundError as e:
        response.status = 404
        return {"status": "error", "message": str(e)}
    except msgspec.JSONDecodeError:
        response.status = 400
        return {"status": "error", "message": "JSON 格式有误"}
    except Exception as e:
        response.status = 500
        return {"status": "error", "message": str(e)}

def main():
    print("服务器正在启动，请访问: http://127.0.0.1:8033")
    run(host='127.0.0.1', server='waitress', port=8033, debug=True, reloader=True) #去掉reloader变为单进程


if __name__ == "__main__":
    main()
