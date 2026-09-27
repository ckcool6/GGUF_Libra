import datetime
from pathlib import Path
import time
from typing import List, Optional, Tuple
import msgspec
import numpy as np
from scipy.sparse import csr_matrix, diags
from scipy.sparse.linalg import eigsh
import random

# 获取上一级目录
CURRENT_DIR = Path(__file__).resolve().parent
PARENT_DIR = CURRENT_DIR.parent

# 忽略 JSON 中多余的字段 
class ChatChain(msgspec.Struct, omit_defaults=True, kw_only=True):
    DialogAbstract: str = ""
    DialogMain: Optional['ChatChain'] = None
    DialogSide: Optional['ChatChain'] = None

class Entry(msgspec.Struct):
    start_id: int
    end_id: int
    # 采用 tuple 提升性能和降低内存
    raw_data: Tuple[str, ...] = () 

class TreeMatrix(msgspec.Struct):
    node_count: int
    entry_count: int
    entries: List[Entry]

class Record(msgspec.Struct):
    logic_t: float
    uuid: int
    date: datetime.datetime  # 修复：指明是 datetime 模块下的 datetime 类
    keyword: str
    matrix: TreeMatrix    
    
import msgpack  # 确保文件顶部有 import msgpack，或者在函数内部使用

def parse_json():
    """解析上一级目录下的 json 文件，并按 logic_t 降序保存到 data.bin"""
    source_json = PARENT_DIR / "chain_history.json"
    target_msgpack = PARENT_DIR / "data.bin"

    if not source_json.exists():
        raise FileNotFoundError(f"未找到原始 JSON 文件: {source_json}")

    start_time = time.time()

    # 直接读取原始文件
    with open(source_json, "rb") as f:
        json_bytes = f.read()

    # 最外层是 {}，type 设为单体 ChatChain
    single_tree: ChatChain = msgspec.json.decode(json_bytes, type=ChatChain)
    
    # 包装成列表，因为 build_entry 需要 List
    rawdata: List[ChatChain] = [single_tree]

    # 执行转换
    entries, total_nodes = build_entry(rawdata) 
    
    matrix = TreeMatrix(
        node_count=total_nodes,
        entry_count=len(entries),
        entries=entries
    )

    logic_t = compute_logic_t(matrix)
    
    # 构建当前新 Record
    final_record = Record(
        logic_t=logic_t,
        uuid=generate_int_uuid(), 
        date=datetime.datetime.now(),
        keyword=extract_keyword(entries, single_tree), 
        matrix=matrix
    )

    print("\n======= 提取出的 Entry 列表详情 =======")
    for i, entry in enumerate(entries):
        print(f"连线 {i+1}: 节点 [{entry.start_id}] ➔ 节点 [{entry.end_id}]")
        print(f"  父亲文本前20字: {entry.raw_data[0][:20].replace('\n', '')}...")
        print(f"  儿子文本前20字: {entry.raw_data[1][:20].replace('\n', '')}...\n")
    print("=======================================\n")
    
    # ========================================================
    # 核心改动：先读旧记录 -> 放入新记录 -> 按 logic_t 降序重排 -> 整体覆写
    # ========================================================
    all_records: List[Record] = []

    # 1. 如果旧文件存在，把历史的 Record 全读出来
    if target_msgpack.exists() and target_msgpack.stat().st_size > 0:
        with open(target_msgpack, "rb") as f:
            unpacker = msgpack.Unpacker(raw=False)
            unpacker.feed(f.read())
            for item in unpacker:
                try:
                    all_records.append(msgspec.convert(item, type=Record))
                except Exception:
                    pass

    # 2. 加入当前这次的新记录
    all_records.append(final_record)

    # 3. 按 logic_t 从大到小排序 (reverse=True)
    all_records.sort(key=lambda r: r.logic_t, reverse=True)

    # 4. 用 "wb" 模式把排好序的全部记录重新写入文件
    with open(target_msgpack, "wb") as f:
        for rec in all_records:
            f.write(msgspec.msgpack.encode(rec))

    elapsed = time.time() - start_time
    
    return {
        "tree_count": len(rawdata), 
        "logic_t": logic_t,
        "entry_count": len(entries), 
        "total_records_in_db": len(all_records), # 新增：告诉你现在库里排了多少条
        "source_size_mb": round(source_json.stat().st_size / (1024 * 1024), 2),
        "packed_size_mb": round(target_msgpack.stat().st_size / (1024 * 1024), 2),
        "elapsed_seconds": round(elapsed, 3)
    }


def build_entry(rawdata: List[ChatChain]) -> Tuple[List[Entry], int]:
    """
    遍历 JSON 中的所有聊天树列表，并将它们整合到一个连续的 Entry 列表中
    """
    all_entries = []
    total_nodes = 0
    current_global_id = 0  # 保证多棵树之间的 ID 是全局连续的

    for root_tree in rawdata:
        # 对每棵树执行栈提取，并传入当前累积的全局 ID
        entries, node_count = extract_by_stack(root_tree, start_global_id=current_global_id)
        
        all_entries.extend(entries)
        total_nodes += node_count
        current_global_id += node_count  # 更新下一棵树的起始 ID

    return all_entries, total_nodes


def extract_by_stack(root_node: ChatChain, start_global_id: int = 0) -> Tuple[List[Entry], int]:
    """
    邻接表提取法：将对话树拆解为“父节点 -> 子节点”的一对一连接关系
    """
    if not root_node:
        return [], 0

    entries: List[Entry] = []
    
    global_id = start_global_id
    
    # 栈里存的是元组：(当前节点对象, 分配给该节点的ID)
    stack = [(root_node, global_id)]
    
    # 根节点自己占用了 1 个 ID，所以 global_id 先 +1
    global_id += 1 
    nodes_processed = 1
    
    while stack:
        current_node, current_id = stack.pop()
        
        # 提取父节点的文本（作为边的起点内容）
        parent_abstract = current_node.DialogAbstract if current_node.DialogAbstract else ""
        
        # 1. 发现支线 (Side)
        if current_node.DialogSide:
            side_id = global_id  # 给支线节点分配新 ID
            global_id += 1
            nodes_processed += 1
            
            side_abstract = current_node.DialogSide.DialogAbstract if current_node.DialogSide.DialogAbstract else ""
            
            # 生成一条从 父亲 -> 支线 的连接 Entry
            entries.append(Entry(
                start_id=current_id,
                end_id=side_id,
                raw_data=(parent_abstract, side_abstract) # 把父子对话包在一起
            ))
            # 把支线压入栈，继续往下找
            stack.append((current_node.DialogSide, side_id))
            
        # 2. 发现主线 (Main)
        if current_node.DialogMain:
            main_id = global_id  # 给主线节点分配新 ID
            global_id += 1
            nodes_processed += 1
            
            main_abstract = current_node.DialogMain.DialogAbstract if current_node.DialogMain.DialogAbstract else ""
            
            # 生成一条从 父亲 -> 主线 的连接 Entry
            entries.append(Entry(
                start_id=current_id,
                end_id=main_id,
                raw_data=(parent_abstract, main_abstract) # 把父子对话包在一起
            ))
            # 把主线压入栈，继续往下找
            stack.append((current_node.DialogMain, main_id))

    return entries, nodes_processed


def compute_logic_t(matrix_struct: TreeMatrix) -> float:
    """计算拉普拉斯第二特征值的倒数 (logic_T)"""
    if matrix_struct.node_count <= 2: return float('inf')
    num_nodes = matrix_struct.node_count
    start_ids = [entry.start_id for entry in matrix_struct.entries]
    end_ids = [entry.end_id for entry in matrix_struct.entries]
    
    # 构造无向图
    row = np.array(start_ids + end_ids)
    col = np.array(end_ids + start_ids)
    data = np.ones(len(row))
    
    A = csr_matrix((data, (row, col)), shape=(num_nodes, num_nodes))
    degrees = np.array(A.sum(axis=1)).flatten()
    D = diags(degrees)
    L = D - A
    
    try:
        # 节点小于 5 时使用稠密矩阵求解器
        if num_nodes < 5:
            dense_L = L.toarray()
            eigenvalues = np.linalg.eigvalsh(dense_L)
            lambda_2 = eigenvalues[1]
        else:
            eigenvalues, _ = eigsh(L.astype(float), k=2, which='SA')
            lambda_2 = eigenvalues[1]

        
        if lambda_2 < 1e-10: return float('inf')
        return float(round(1.0 / lambda_2, 4))
    except Exception as e:
        print(f"计算特征值出错: {e}")
        return 0.0


def generate_int_uuid() -> int:
    """生成一个保证在 int64 范围内的有序唯一 ID (毫秒时间戳 + 随机尾缀)"""
    # 41位时间戳(毫秒) + 22位随机数 = 63位整数 (最高位为0，保证为正数)
    timestamp_ms = int(time.time() * 1000)
    rand_suffix = random.getrandbits(22)
    return (timestamp_ms << 22) | rand_suffix

def extract_keyword(entries: List[Entry], root_tree: ChatChain) -> str:
    # 1. 优先拿整棵树最后遍历到的父子总结（不管是主线还是支线的终点）
    if entries and len(entries[-1].raw_data) > 1:
        return entries[-1].raw_data[1].strip()
    
    # 2. 兜底保护：如果树只有根节点一个点，没有连线 (entries 为空)
    if root_tree and root_tree.DialogAbstract:
        return root_tree.DialogAbstract.strip()
        
    return "Untitled Chat"


# test
if __name__ == "__main__":
    try:
        print(f"正在处理数据，目标目录: {PARENT_DIR}")
        result = parse_json()
        
        print("\n✅ 处理成功！统计信息如下：")
        print("-" * 30)
        print(f"🌲 树的数量       : {result['tree_count']} 棵")
        print(f"🔗 提取的父子连线 : {result['entry_count']} 对")
        print(f"✨ Logic T 值     : {result['logic_t']}")  
        print(f"📁 原始 JSON 大小 : {result['source_size_mb']} MB")
        print(f"📦 压缩 Msgpack 大小: {result['packed_size_mb']} MB")
        print(f"⏱️  耗时           : {result['elapsed_seconds']} 秒")
        print("-" * 30)
        
    except FileNotFoundError as e:
        print(f"❌ 错误: {e}")
    except msgspec.ValidationError as e:
        print(f"❌ JSON 格式校验失败: {e}")
    except Exception as e:
        print(f"❌ 发生未知错误: {e}")