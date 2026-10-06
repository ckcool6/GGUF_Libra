# Copyright 2026 Lu ZhiYuan
# SPDX-License-Identifier: AGPL-3.0-only

import datetime
from pathlib import Path
import time
from typing import List, Optional, Tuple
import msgspec
import numpy as np
from scipy.sparse import csr_matrix, diags
from scipy.sparse.linalg import eigsh
import random
import sys

if getattr(sys, 'frozen', False):
    CURRENT_DIR = Path(sys.executable).resolve().parent
else:
    CURRENT_DIR = Path(__file__).resolve().parent

# Get parent directory
PARENT_DIR = CURRENT_DIR.parent

# Ignore extra fields in JSON
class ChatChain(msgspec.Struct, omit_defaults=True, kw_only=True):
    DialogAbstract: str = ""
    DialogMain: Optional['ChatChain'] = None
    DialogSide: Optional['ChatChain'] = None

class Entry(msgspec.Struct):
    start_id: int
    end_id: int
    # Use tuple to improve performance and reduce memory usage
    raw_data: Tuple[str, ...] = () 

class TreeMatrix(msgspec.Struct):
    node_count: int
    entry_count: int
    entries: List[Entry]

class Record(msgspec.Struct):
    logic_t: float
    uuid: int
    date: datetime.datetime  # Explicitly using datetime class from datetime module
    keyword: str
    matrix: TreeMatrix    
    
import msgpack

def parse_json():
    """Parse JSON file from parent directory and save to data.bin sorted by logic_t descending."""
    source_json = PARENT_DIR / "chain_history.json"
    target_msgpack = PARENT_DIR / "data.bin"

    if not source_json.exists():
        raise FileNotFoundError(f"Source JSON file not found: {source_json}")

    start_time = time.time()

    # Read raw source file directly
    with open(source_json, "rb") as f:
        json_bytes = f.read()

    # Root object is {}, typed as a single ChatChain
    single_tree: ChatChain = msgspec.json.decode(json_bytes, type=ChatChain)
    
    # Wrap into list as build_entry expects a List
    rawdata: List[ChatChain] = [single_tree]

    # Execute conversion
    entries, total_nodes = build_entry(rawdata) 
    
    if total_nodes < 2 or len(entries) == 0:
        elapsed = time.time() - start_time
        print("\033[93m[WARN]\033[0m Conversation has no messages/edges, skipped saving.")
        return {
            "status": "skipped",
            "reason": "empty_conversation",
            "tree_count": len(rawdata),
            "entry_count": 0,
            "logic_t": 0.0,
            "source_size_mb": round(source_json.stat().st_size / (1024 * 1024), 2),
            "packed_size_mb": round(target_msgpack.stat().st_size / (1024 * 1024), 2) if target_msgpack.exists() else 0.0,
            "elapsed_seconds": round(elapsed, 3)
        }
    
    matrix = TreeMatrix(
        node_count=total_nodes,
        entry_count=len(entries),
        entries=entries
    )

    logic_t = compute_logic_t(matrix)
    
    # Build current new Record
    final_record = Record(
        logic_t=logic_t,
        uuid=generate_int_uuid(), 
        date=datetime.datetime.now(),
        keyword=extract_keyword(entries, single_tree), 
        matrix=matrix
    )

    print("\n======= Extracted Entry List Details =======")
    for i, entry in enumerate(entries):
        print(f"Edge {i+1}: Node [{entry.start_id}] ➔ Node [{entry.end_id}]")
        print(f"  Parent text (first 20 chars): {entry.raw_data[0][:20].replace('\n', '')}...")
        print(f"  Child text (first 20 chars): {entry.raw_data[1][:20].replace('\n', '')}...\n")
    print("============================================\n")
    
    # ========================================================
    # Core flow: read existing records -> append new record -> sort by logic_t descending -> overwrite file
    # ========================================================
    all_records: List[Record] = []

    # If old file exists, read all historical records
    if target_msgpack.exists() and target_msgpack.stat().st_size > 0:
        with open(target_msgpack, "rb") as f:
            unpacker = msgpack.Unpacker(raw=False)
            unpacker.feed(f.read())
            for item in unpacker:
                try:
                    all_records.append(msgspec.convert(item, type=Record))
                except Exception:
                    pass

    # Append the current new record
    all_records.append(final_record)

    # Sort by logic_t descending (reverse=True)
    all_records.sort(key=lambda r: r.logic_t, reverse=True)

    # Overwrite all sorted records back to file in 'wb' mode
    with open(target_msgpack, "wb") as f:
        for rec in all_records:
            f.write(msgspec.msgpack.encode(rec))

    elapsed = time.time() - start_time
    
    return {
        "tree_count": len(rawdata), 
        "logic_t": logic_t,
        "entry_count": len(entries), 
        "total_records_in_db": len(all_records),
        "source_size_mb": round(source_json.stat().st_size / (1024 * 1024), 2),
        "packed_size_mb": round(target_msgpack.stat().st_size / (1024 * 1024), 2),
        "elapsed_seconds": round(elapsed, 3)
    }


def build_entry(rawdata: List[ChatChain]) -> Tuple[List[Entry], int]:
    """
    Traverse all chat trees in rawdata and aggregate them into a sequential Entry list.
    """
    all_entries = []
    total_nodes = 0
    current_global_id = 0  # Ensure globally continuous node IDs across multiple trees

    for root_tree in rawdata:
        # Extract entries for each tree using stack traversal with current global ID offset
        entries, node_count = extract_by_stack(root_tree, start_global_id=current_global_id)
        
        all_entries.extend(entries)
        total_nodes += node_count
        current_global_id += node_count  # Update starting ID for the next tree

    return all_entries, total_nodes


def extract_by_stack(root_node: ChatChain, start_global_id: int = 0) -> Tuple[List[Entry], int]:
    """
    Adjacency extraction: decomposes conversation tree into parent-child directed edges.
    """
    if not root_node:
        return [], 0

    entries: List[Entry] = []
    
    global_id = start_global_id
    
    # Stack items: (current_node, assigned_node_id)
    stack = [(root_node, global_id)]
    
    # Root node occupies 1 ID, increment global_id
    global_id += 1 
    nodes_processed = 1
    
    while stack:
        current_node, current_id = stack.pop()
        
        # Extract parent text (source content for the edge)
        parent_abstract = current_node.DialogAbstract if current_node.DialogAbstract else ""
        
        # Handle side branch (Side)
        if current_node.DialogSide:
            side_id = global_id  # Assign new ID to side node
            global_id += 1
            nodes_processed += 1
            
            side_abstract = current_node.DialogSide.DialogAbstract if current_node.DialogSide.DialogAbstract else ""
            
            # Create edge entry from Parent -> Side
            entries.append(Entry(
                start_id=current_id,
                end_id=side_id,
                raw_data=(parent_abstract, side_abstract)  # Bundle parent and child text together
            ))
            # Push side branch to stack to continue traversal
            stack.append((current_node.DialogSide, side_id))
            
        # Handle main branch (Main)
        if current_node.DialogMain:
            main_id = global_id  # Assign new ID to main node
            global_id += 1
            nodes_processed += 1
            
            main_abstract = current_node.DialogMain.DialogAbstract if current_node.DialogMain.DialogAbstract else ""
            
            # Create edge entry from Parent -> Main
            entries.append(Entry(
                start_id=current_id,
                end_id=main_id,
                raw_data=(parent_abstract, main_abstract)  # Bundle parent and child text together
            ))
            # Push main branch to stack to continue traversal
            stack.append((current_node.DialogMain, main_id))

    return entries, nodes_processed


def compute_logic_t(matrix_struct: TreeMatrix) -> float:
    """Compute the reciprocal of the second smallest Laplacian eigenvalue (logic_T)."""
    if matrix_struct.node_count < 2 or matrix_struct.entry_count == 0: 
        return 0.0
    num_nodes = matrix_struct.node_count
    start_ids = [entry.start_id for entry in matrix_struct.entries]
    end_ids = [entry.end_id for entry in matrix_struct.entries]
    
    # Construct undirected graph
    row = np.array(start_ids + end_ids)
    col = np.array(end_ids + start_ids)
    data = np.ones(len(row))
    
    A = csr_matrix((data, (row, col)), shape=(num_nodes, num_nodes))
    degrees = np.array(A.sum(axis=1)).flatten()
    D = diags(degrees)
    L = D - A
    
    try:
        # Use dense solver when node count is less than 5
        if num_nodes < 5:
            dense_L = L.toarray()
            eigenvalues = np.linalg.eigvalsh(dense_L)
            lambda_2 = eigenvalues[1]
        else:
            eigenvalues, _ = eigsh(L.astype(float), k=2, which='SA')
            lambda_2 = eigenvalues[1]

        if lambda_2 < 1e-10: 
            return float('inf')
        return float(round(1.0 / lambda_2, 4))
    except Exception as e:
        print(f"Error computing eigenvalues: {e}")
        return 0.0


def generate_int_uuid() -> int:
    """Generate an ordered unique ID within int64 range (millisecond timestamp + random suffix)."""
    # 41-bit timestamp (ms) + 22-bit random suffix = 63-bit signed integer (sign bit is 0)
    timestamp_ms = int(time.time() * 1000)
    rand_suffix = random.getrandbits(22)
    return (timestamp_ms << 22) | rand_suffix

def extract_keyword(entries: List[Entry], root_tree: ChatChain) -> str:
    # Prefer summary from the last traversed edge (endpoint of main or side branch)
    if entries and len(entries[-1].raw_data) > 1:
        return entries[-1].raw_data[1].strip()
    
    # Fallback: if tree has only a root node with no edges (entries is empty)
    if root_tree and root_tree.DialogAbstract:
        return root_tree.DialogAbstract.strip()
        
    return "Untitled Chat"


# Test runner
if __name__ == "__main__":
    try:
        print(f"Processing data in target directory: {PARENT_DIR}")
        result = parse_json()
        
        if result.get("status") == "skipped":
            print(f"\n[INFO] Skipped saving: {result.get('reason')}")
        else:
            print("\n[OK] Processed successfully! Statistics:")
            print("-" * 30)
            print(f"Tree Count        : {result['tree_count']}")
            print(f"Parent-Child Edges: {result['entry_count']}")
            print(f"Logic T Value     : {result['logic_t']}")  
            print(f"Source JSON Size  : {result['source_size_mb']} MB")
            print(f"Msgpack Size      : {result['packed_size_mb']} MB")
            print(f"Elapsed Time      : {result['elapsed_seconds']} s")
            print("-" * 30)
        
    except FileNotFoundError as e:
        print(f"Error: {e}")
    except msgspec.ValidationError as e:
        print(f"JSON validation failed: {e}")
    except Exception as e:
        print(f"Unexpected error occurred: {e}")