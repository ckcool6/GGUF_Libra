# Copyright 2026 Lu ZhiYuan
# SPDX-License-Identifier: AGPL-3.0-only

import cmd
import json
from typing import List
import msgspec
import parser
import os 

# ==========================================
# TUI Interactive Console
# ==========================================
class AppTUI(cmd.Cmd):
    # ANSI Color & Style Codes
    RED = '\033[91m'
    GREEN = '\033[92m'
    YELLOW = '\033[93m'
    BLUE = '\033[94m'
    MAGENTA = '\033[95m'
    CYAN = '\033[96m'
    WHITE = '\033[97m'
    GRAY = '\033[90m'
    BOLD = '\033[1m'
    DIM = '\033[2m'
    RESET = '\033[0m'

    # Standardized Colored CLI Badges
    TAG_OK = f"{GREEN}[OK]{RESET}"
    TAG_ERR = f"{RED}[ERROR]{RESET}"
    TAG_INFO = f"{CYAN}[INFO]{RESET}"
    TAG_WARN = f"{YELLOW}[WARN]{RESET}"

    prompt = 'my-app> '

    intro = (
        f"{BOLD}Welcome to the Console!{RESET}\n\n"
        f"Type {BOLD}\"help\"{RESET} to see available commands, or {BOLD}\"exit\"{RESET} to quit."
    )

    def do_status(self, arg):
        """Check the running status: status"""
        print(f"{self.TAG_OK} Console application is running normally.")

    def do_save(self, arg):
        """Execute the save logic directly: save"""
        print(f"{self.TAG_INFO} Executing save logic...")
        try:
            result = parser.parse_json()
            if result.get("status") == "skipped":
                print(f"{self.TAG_WARN} Conversation is empty, save skipped.")
            else:
                print(f"{self.TAG_OK} Parsed and saved successfully:\n{json.dumps(result, indent=2, ensure_ascii=False)}")
        except Exception as e:
            print(f"{self.TAG_ERR} Execution failed: {e}")

    def do_data(self, arg):
        """Inspect internal storage status of data.bin: data [count|all]
        Usage:
          data        -> Show top 5 records (default)
          data 10     -> Show top 10 records
          data all    -> Show all records
        """
        bin_path = parser.PARENT_DIR / "data.bin"

        if not bin_path.exists():
            print(f"{self.TAG_ERR} Binary file not found: {bin_path}")
            return

        file_size = bin_path.stat().st_size
        print(f"\n{self.BOLD}==================== data.bin Storage Overview ===================={self.RESET}")
        print(f"  File Path     : {self.CYAN}{bin_path.resolve()}{self.RESET}")
        print(f"  File Size     : {self.YELLOW}{file_size}{self.RESET} bytes ({self.YELLOW}{round(file_size / 1024, 2)} KB{self.RESET})")

        if file_size == 0:
            print(f"{self.TAG_WARN} File is empty!")
            return

        # ----------------------------------------------------
        # 1. Stream unpack all historical records using msgpack.Unpacker
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
            except Exception:
                pass

        total_count = len(records)
        print(f"  Total Records : {self.BOLD}{self.GREEN}{total_count}{self.RESET} conversation tree(s)")
        print(f"{self.BOLD}==================================================================={self.RESET}\n")

        # ----------------------------------------------------
        # 2. Parse arguments to determine display count (Pagination / slicing logic)
        # ----------------------------------------------------
        arg = arg.strip().lower()
        if arg == 'all':
            display_limit = total_count
        elif arg.isdigit():
            display_limit = min(int(arg), total_count)
        else:
            # Default to showing top 5 records
            display_limit = min(5, total_count)

        # Slice the records to the requested limit
        display_records = records[:display_limit]

        # ----------------------------------------------------
        # 3. Print structured details for the selected records (Full Color)
        # ----------------------------------------------------
        for idx, rec in enumerate(display_records):
            print(f"{self.BOLD}{self.CYAN}[RECORD #{idx + 1}]{self.RESET}")
            # UUID in Magenta / Purple
            print(f"  {self.GRAY}UUID      :{self.RESET} {self.MAGENTA}{rec.uuid}{self.RESET}")
            # Timestamp in Blue
            print(f"  {self.GRAY}Timestamp :{self.RESET} {self.BLUE}{rec.date.strftime('%Y-%m-%d %H:%M:%S')}{self.RESET}")
            # Logic T in Bold Yellow
            print(f"  {self.GRAY}Logic T   :{self.RESET} {self.BOLD}{self.YELLOW}{rec.logic_t}{self.RESET}")
            # Keyword in Bright Green
            print(f"  {self.GRAY}Keyword   :{self.RESET} {self.GREEN}{rec.keyword}{self.RESET}")
            # Topology metrics in Cyan
            print(f"  {self.GRAY}Topology  :{self.RESET} {self.CYAN}{rec.matrix.node_count}{self.RESET} nodes, {self.CYAN}{rec.matrix.entry_count}{self.RESET} parent-child edges")
            
            # If edges exist, display the colored path
            if rec.matrix.entries:
                last_entry = rec.matrix.entries[-1]
                p_text = last_entry.raw_data[0][:20].replace('\n', '')
                c_text = last_entry.raw_data[1][:20].replace('\n', '')
                # Node IDs in Yellow, Arrow highlighted, Text in Dim/Normal
                node_from = f"[{self.YELLOW}{last_entry.start_id}{self.RESET}]"
                node_to = f"[{self.YELLOW}{last_entry.end_id}{self.RESET}]"
                arrow = f"{self.BOLD}{self.CYAN}->{self.RESET}"
                print(f"  {self.GRAY}Terminal  :{self.RESET} {node_from} {self.DIM}{p_text}...{self.RESET} {arrow} {node_to} {c_text}...")
            
            print(f"{self.GRAY}{'-' * 52}{self.RESET}")

        # Notify user if some records are hidden
        if display_limit < total_count:
            print(f"{self.TAG_INFO} Showing {display_limit} of {total_count} records. Use 'data all' or 'data <number>' to see more.\n")

        # ----------------------------------------------------
        # 4. Raw hex slice preview (first 64 bytes)
        # ----------------------------------------------------
        print(f"{self.BLUE}[DUMP] Raw Binary Slice Preview (First 64 Bytes Hex & ASCII):{self.RESET}")
        preview_bytes = raw_bytes[:64]
        for i in range(0, len(preview_bytes), 16):
            chunk = preview_bytes[i:i+16]
            hex_str = " ".join(f"{b:02X}" for b in chunk).ljust(48)
            # Display printable ASCII characters; non-printable characters shown as '.'
            ascii_str = "".join(chr(b) if 32 <= b <= 126 else "." for b in chunk)
            print(f"  {self.GRAY}{i:04X}{self.RESET} | {self.YELLOW}{hex_str}{self.RESET}| {self.CYAN}{ascii_str}{self.RESET}")
        print()

    # 
    def do_export(self, arg):
        """Export top N records to a JSON file in current directory: export [count|all] [filename]
        Usage:
          export                  -> Export top 5 records to exported_records.json
          export 10               -> Export top 10 records to exported_records.json
          export all              -> Export all records to exported_records.json
          export 3 custom.json    -> Export top 3 records to custom.json
        """
        bin_path = parser.PARENT_DIR / "data.bin"

        if not bin_path.exists() or bin_path.stat().st_size == 0:
            print(f"{self.TAG_ERR} Binary file not found or empty: {bin_path}")
            return

        # ----------------------------------------------------
        # 1. Parse arguments (count and target filename)
        # ----------------------------------------------------
        parts = arg.strip().split()
        count_arg = parts[0].lower() if parts else "5"
        output_filename = parts[1] if len(parts) > 1 else "exported_records.json"
        
        # Save directly to CURRENT_DIR instead of PARENT_DIR
        output_path = parser.CURRENT_DIR / output_filename

        # ----------------------------------------------------
        # 2. Stream unpack records from data.bin
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
            except Exception:
                pass

        total_count = len(records)
        if total_count == 0:
            print(f"{self.TAG_WARN} No valid records found to export.")
            return

        # ----------------------------------------------------
        # 3. Determine slice limit
        # ----------------------------------------------------
        if count_arg == 'all':
            limit = total_count
        elif count_arg.isdigit():
            limit = min(int(count_arg), total_count)
        else:
            limit = min(5, total_count)

        target_records = records[:limit]

        # ----------------------------------------------------
        # 4. Serialize to formatted JSON using msgspec
        # ----------------------------------------------------
        try:
            # Encode Record structs directly to JSON bytes
            json_bytes = msgspec.json.encode(target_records)
            # Pretty-print JSON with 2-space indentation
            formatted_json = msgspec.json.format(json_bytes, indent=2)

            with open(output_path, "wb") as f:
                f.write(formatted_json)

            exported_size = output_path.stat().st_size
            print(f"{self.TAG_OK} Successfully exported {self.GREEN}{len(target_records)}{self.RESET} record(s)!")
            print(f"  Destination : {self.CYAN}{output_path.resolve()}{self.RESET}")
            print(f"  File Size   : {self.YELLOW}{round(exported_size / 1024, 2)} KB{self.RESET}")
        except Exception as e:
            print(f"{self.TAG_ERR} Failed to export records: {e}")

    #
    def do_import(self, arg):
        """Import records from a JSON file into data.bin: import [filename]
        Usage:
          import                  -> Import from exported_records.json (in current dir)
          import backup.json      -> Import from custom JSON file
        """
        filename = arg.strip() if arg.strip() else "exported_records.json"
        
        # Priority: Current directory first, then fallback to relative/absolute path
        json_path = parser.CURRENT_DIR / filename
        if not json_path.exists():
            import pathlib
            json_path = pathlib.Path(filename)
            if not json_path.exists():
                print(f"{self.TAG_ERR} JSON file not found: {filename}")
                return

        print(f"{self.TAG_INFO} Reading from: {self.CYAN}{json_path.resolve()}{self.RESET}")

        # ----------------------------------------------------
        # 1. Tolerant Struct to handle possible `logic_t: null`
        # ----------------------------------------------------
        from typing import Optional
        import datetime

        class FlexibleRecord(msgspec.Struct, omit_defaults=True):
            uuid: int
            date: datetime.datetime
            keyword: str
            matrix: parser.TreeMatrix
            logic_t: Optional[float] = 0.0

        # Read JSON file
        try:
            with open(json_path, "rb") as f:
                raw_json = f.read()

            # Attempt to decode as list, fallback to single record if not a list
            try:
                imported_items = msgspec.json.decode(raw_json, type=List[FlexibleRecord])
            except Exception:
                single_item = msgspec.json.decode(raw_json, type=FlexibleRecord)
                imported_items = [single_item]
        except Exception as e:
            print(f"{self.TAG_ERR} Failed to parse JSON format: {e}")
            return

        if not imported_items:
            print(f"{self.TAG_WARN} No records found in JSON.")
            return

        # ----------------------------------------------------
        # 2. Read existing records from data.bin
        # ----------------------------------------------------
        bin_path = parser.PARENT_DIR / "data.bin"
        existing_records: List[parser.Record] = []

        if bin_path.exists() and bin_path.stat().st_size > 0:
            with open(bin_path, "rb") as f:
                raw_bytes = f.read()
            import msgpack
            unpacker = msgpack.Unpacker(raw=False)
            unpacker.feed(raw_bytes)
            for item in unpacker:
                try:
                    existing_records.append(msgspec.convert(item, type=parser.Record))
                except Exception:
                    pass

        # ----------------------------------------------------
        # 3. Deduplicate and merge by UUID
        # ----------------------------------------------------
        records_map = {r.uuid: r for r in existing_records}
        added_count = 0
        updated_count = 0

        for item in imported_items:
            # Handle null logic_t gracefully (treat null as 0.0)
            norm_logic_t = item.logic_t if item.logic_t is not None else 0.0

            clean_rec = parser.Record(
                logic_t=norm_logic_t,
                uuid=item.uuid,
                date=item.date,
                keyword=item.keyword,
                matrix=item.matrix
            )

            if item.uuid in records_map:
                updated_count += 1
            else:
                added_count += 1

            records_map[item.uuid] = clean_rec

        # ----------------------------------------------------
        # 4. Sort descending by logic_t and write back
        # ----------------------------------------------------
        all_records = list(records_map.values())
        all_records.sort(key=lambda r: r.logic_t, reverse=True)

        try:
            with open(bin_path, "wb") as f:
                for rec in all_records:
                    f.write(msgspec.msgpack.encode(rec))

            print(f"{self.TAG_OK} Records imported successfully!")
            print(f"  New Added     : {self.GREEN}{added_count}{self.RESET}")
            print(f"  Overwritten   : {self.YELLOW}{updated_count}{self.RESET}")
            print(f"  Total in DB   : {self.CYAN}{len(all_records)}{self.RESET}")
        except Exception as e:
            print(f"{self.TAG_ERR} Failed to save back to data.bin: {e}")

    #        
    def do_clear(self, arg):
        """Clear the terminal screen: clear"""
        # Cross-platform terminal clear (Windows: cls, Unix/macOS: clear)
        os.system('cls' if os.name == 'nt' else 'clear')

        # Alias for convenience (Windows muscle memory)
    do_cls = do_clear

    #
    def do_exit(self, arg):
        """Exit the program: exit"""
        print(f"{self.TAG_INFO} Goodbye!")
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
        print(f"\n{AppTUI.RED}[ABORT]{AppTUI.RESET} Forced exit...")

if __name__ == "__main__":
    main()