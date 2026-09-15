import threading
import logging
import cmd
import requests
import json
import msgspec
from bottle import response, route, run
from parser import parse_json  

# ==========================================
# Configure logging: Silence the server output and write to a log file
# ==========================================
logging.basicConfig(
    filename='server.log',
    level=logging.INFO,
    format='%(asctime)s [%(levelname)s] - %(message)s'
)
# Specifically capture Waitress logs and write to the file
logging.getLogger('waitress').setLevel(logging.INFO)


# ==========================================
# Routes
# ==========================================
@route('/')
def index():
    return "<h1>server running...</h1>"

@route('/api/test')
def api_test():
    return {"message": "Bottle server is running normally!", "status": "success"}

@route('/api/save', method=['POST'])
def api_save():
    try:
        result = parse_json()
        return {"status": "success", "message": "Parsed and saved successfully", "data": result}
    except FileNotFoundError as e:
        response.status = 404
        return {"status": "error", "message": str(e)}
    except (msgspec.DecodeError, msgspec.ValidationError) as e:
        response.status = 400
        return {"status": "error", "message": f"Invalid JSON format or structure: {e}"}
    except Exception as e:
        response.status = 500
        return {"status": "error", "message": str(e)}


# ==========================================
# Background server runner function
# ==========================================
def run_server():
    run(host='127.0.0.1', port=8033, server='waitress', quiet=True)

# ==========================================
# TUI Interactive Console
# ==========================================
class ServerTUI(cmd.Cmd):
    GREEN = '\033[92m'
    BLUE = '\033[94m'
    CYAN = '\033[96m'
    BOLD = '\033[1m'
    RESET = '\033[0m'

    prompt = 'my-server> '

    intro = (
        f"{BOLD}Welcome to the Server Console!{RESET}\n\n"
        f"{GREEN}Web server is running silently in the background.{RESET}\n"
        f"Please visit: {CYAN}http://127.0.0.1:8033{RESET}\n"
        f"Type {BOLD}\"help\" to see available commands, or \"exit\" to quit.{RESET}"
    )

    def do_status(self, arg):
        """Check the running status of the background server: status"""
        try:
            res = requests.get("http://127.0.0.1:8033/api/test")
            print(f"✅ Server is online! Response: {res.json()['message']}")
        except Exception as e:
            print(f"❌ Cannot connect to the server: {e}")

    def do_save(self, arg):
        """Test the save logic: save"""
        print("Calling /api/save endpoint...")
        try:
            res = requests.post("http://127.0.0.1:8033/api/save")
            print(f"📦 Response result:\n{json.dumps(res.json(), indent=2, ensure_ascii=False)}")
        except Exception as e:
            print(f"❌ Call failed: {e}")

    def do_exit(self, arg):
        """Exit the program and stop the server: exit"""
        print("Shutting down the server. Bye! 👋")
        return True # Returning True ends the Cmd loop


# ==========================================
# Main program: Put it all together
# ==========================================
def main():
    # Start a daemon thread to run the Web server (daemon=True)
    server_thread = threading.Thread(target=run_server, daemon=True)
    server_thread.start()

    # Start the foreground TUI
    try:
        ServerTUI().cmdloop()
    except KeyboardInterrupt:
        # Catch Ctrl+C
        print("\nForced exit...")

if __name__ == "__main__":
    main()