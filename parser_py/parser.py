import datetime
from pathlib import Path
import time
from typing import List, Optional, Tuple
import msgspec

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
    
def parse_json():
    """解析上一级目录下的 json 文件"""
    source_json = PARENT_DIR / "chain_history.json"
    target_msgpack = PARENT_DIR / "data.bin"

    if not source_json.exists():
        raise FileNotFoundError(f"未找到原始 JSON 文件: {source_json}")

    start_time = time.time()

    # 直接读取原始文件
    with open(source_json, "rb") as f:
        json_bytes = f.read()

    # 关键修改：最外层是 {}，所以 type 设为单体 ChatChain
    single_tree: ChatChain = msgspec.json.decode(json_bytes, type=ChatChain)
    
    # 包装成列表，因为我们后面的 build_entry 需要 List
    rawdata: List[ChatChain] = [single_tree]

    # 执行转换
    entries, total_nodes = build_entry(rawdata) 
    
    matrix = TreeMatrix(
        node_count=total_nodes,
        entry_count=len(entries),
        entries=entries
    )

    # 假定这些函数的实现
    logic_t = compute_logic_t(matrix)
    
    # 构建最终 Record
    final_record = Record(
        logic_t=logic_t,
        uuid=123456789, # 需要你的生成逻辑
        date=datetime.datetime.now(),
        keyword="example_keyword", # 需要你的提取逻辑
        matrix=matrix
    )

    print("\n======= 提取出的 Entry 列表详情 =======")
    for i, entry in enumerate(entries):
        print(f"连线 {i+1}: 节点 [{entry.start_id}] ➔ 节点 [{entry.end_id}]")
        print(f"  父亲文本前20字: {entry.raw_data[0][:20].replace('\n', '')}...")
        print(f"  儿子文本前20字: {entry.raw_data[1][:20].replace('\n', '')}...\n")
    print("=======================================\n")
    
    # 编码为 Msgpack
    packed_bytes = msgspec.msgpack.encode(final_record)

    with open(target_msgpack, "wb") as f:
        f.write(packed_bytes)

    elapsed = time.time() - start_time
    
    return {
        "tree_count": len(rawdata), # 这里现在是 1
        "entry_count": len(entries), 
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

def compute_logic_t(matrix: TreeMatrix) -> float:
    # 你的计算逻辑
    return 0.99


# test
if __name__ == "__main__":
    try:
        print(f"正在处理数据，目标目录: {PARENT_DIR}")
        result = parse_json()
        
        print("\n✅ 处理成功！统计信息如下：")
        print("-" * 30)
        print(f"🌲 树的数量       : {result['tree_count']} 棵")
        print(f"🔗 提取的父子连线 : {result['entry_count']} 对")
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