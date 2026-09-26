import logging
import cmd
import json
import msgspec
from parser import parse_json  

# ==========================================
# Configure logging: Write to a log file
# ==========================================
logging.basicConfig(
    filename='app.log',
    level=logging.INFO,
    format='%(asctime)s [%(levelname)s] - %(message)s'
)

# ==========================================
# TUI Interactive Console
# ==========================================
class AppTUI(cmd.Cmd):
    GREEN = '\033[92m'
    BLUE = '\033[94m'
    CYAN = '\033[96m'
    BOLD = '\033[1m'
    RESET = '\033[0m'

    prompt = 'my-app> '

    intro = (
        f"{BOLD}Welcome to the Console!{RESET}\n\n"
        f"Type {BOLD}\"help\" to see available commands, or \"exit\" to quit.{RESET}"
    )

    def do_status(self, arg):
        """Check the running status: status"""
        print("✅ Console application is running normally!")

    def do_save(self, arg):
        """Execute the save logic directly: save"""
        print("Executing save logic...")
        try:
            # 直接调用本地解析函数
            result = parse_json()
            print(f"📦 Parsed and saved successfully:\n{json.dumps(result, indent=2, ensure_ascii=False)}")
        except FileNotFoundError as e:
            print(f"❌ File not found error: {e}")
        except (msgspec.DecodeError, msgspec.ValidationError) as e:
            print(f"❌ Invalid JSON format or structure: {e}")
        except Exception as e:
            print(f"❌ Execution failed: {e}")

    def do_exit(self, arg):
        """Exit the program: exit"""
        print("Bye! 👋")
        return True 


# ==========================================
# Main program
# ==========================================
def main():
    # Start the foreground TUI directly
    try:
        AppTUI().cmdloop()
    except KeyboardInterrupt:
        # Catch Ctrl+C
        print("\nForced exit...")

if __name__ == "__main__":
    main()