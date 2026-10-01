# Copyright 2026 Lu ZhiYuan
# SPDX-License-Identifier: AGPL-3.0-only

import logging
import cmd
import json
from typing import List
import msgspec
import parser

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
            # Call local parser function directly
            result = parser.parse_json()
            print(f"📦 Parsed and saved successfully:\n{json.dumps(result, indent=2, ensure_ascii=False)}")
        except FileNotFoundError as e:
            print(f"❌ File not found error: {e}")
        except (msgspec.DecodeError, msgspec.ValidationError) as e:
            print(f"❌ Invalid JSON format or structure: {e}")
        except Exception as e:
            print(f"❌ Execution failed: {e}")

    def do_data(self, arg):
        """Inspect internal storage status of data.bin: data"""
        bin_path = parser.PARENT_DIR / "data.bin"

        if not bin_path.exists():
            print(f"❌ Binary file not found: {bin_path}")
            return

        file_size = bin_path.stat().st_size
        print(f"\n{self.BOLD}========== 📄 data.bin Storage Overview =========={self.RESET}")
        print(f"📁 File Path : {bin_path.resolve()}")
        print(f"📊 File Size : {file_size} bytes ({round(file_size / 1024, 2)} KB)")

        if file_size == 0:
            print("⚠️ File is empty!")
            return

        # ----------------------------------------------------
        # Stream unpack all historical records using msgpack.Unpacker
        # ----------------------------------------------------
        records: List[parser.Record] = []
        with open(bin_path, "rb") as f:
            raw_bytes = f.read()

        import msgpack
        unpacker = msgpack.Unpacker(raw=False)
        unpacker.feed(raw_bytes)
        for item in unpacker:
            try:
                rec = msgspec.convert(item, type=parser.Record)
                records.append(rec)
            except Exception as e:
                pass

        print(f"📦 Total Records : {self.GREEN}{len(records)}{self.RESET} conversation tree(s) (Record)")
        print(f"{self.BOLD}=================================================={self.RESET}\n")

        # ----------------------------------------------------
        # 2. List structured details for each Record
        # ----------------------------------------------------
        for idx, rec in enumerate(records):
            print(f"{self.CYAN}【Record #{idx + 1}】{self.RESET}")
            print(f"  🔑 UUID      : {rec.uuid}")
            print(f"  🕒 Timestamp : {rec.date.strftime('%Y-%m-%d %H:%M:%S')}")
            print(f"  ✨ Logic T   : {self.BOLD}{rec.logic_t}{self.RESET}")
            print(f"  🏷️  Keyword   : {self.GREEN}{rec.keyword}{self.RESET}")
            print(f"  🌲 Topology  : {rec.matrix.node_count} nodes, {rec.matrix.entry_count} parent-child edges")
            
            # If edges exist, display the last edge (terminal converged summary)
            if rec.matrix.entries:
                last_entry = rec.matrix.entries[-1]
                p_text = last_entry.raw_data[0][:20].replace('\n', '')
                c_text = last_entry.raw_data[1][:20].replace('\n', '')
                print(f"  🔗 Terminal  : [{last_entry.start_id}] {p_text}... ➔ [{last_entry.end_id}] {c_text}...")
            print("-" * 42)

        # ----------------------------------------------------
        # Raw hex slice preview (first 64 bytes)
        # ----------------------------------------------------
        print(f"\n{self.BLUE}🔍 Raw Binary Slice Preview (First 64 Bytes Hex & ASCII):{self.RESET}")
        preview_bytes = raw_bytes[:64]
        for i in range(0, len(preview_bytes), 16):
            chunk = preview_bytes[i:i+16]
            hex_str = " ".join(f"{b:02X}" for b in chunk).ljust(48)
            # Display printable ASCII characters; non-printable characters shown as '.'
            ascii_str = "".join(chr(b) if 32 <= b <= 126 else "." for b in chunk)
            print(f"  {i:04X} | {hex_str} | {ascii_str}")
        print()

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