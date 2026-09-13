from pathlib import Path
import time
import msgspec

# 获取上一级目录
CURRENT_DIR = Path(__file__).resolve().parent
PARENT_DIR = CURRENT_DIR.parent

def parse_json():
    """解析上一级目录下的 json 文件"""
    
    # 假设文件名为 data.json（按实际文件名修改）
    source_json = PARENT_DIR / "data.json"
    target_msgpack = PARENT_DIR / "data.msgpack"

    if not source_json.exists():
        raise FileNotFoundError(f"未找到原始 JSON 文件: {source_json}")

    start_time = time.time()
    
     # 以二进制流读取 JSON（避免文本解码耗时）
    with open(source_json, "rb") as f:
        json_bytes = f.read()

    # C 级别极速解析为 Struct 对象列表
    # 这里假设外层是一个列表 [ {...}, {...} ]，按实际结构指定 type
    data: list[Item] = msgspec.json.decode(json_bytes, type=list[Item])

    # 这里可以对 data 列表进行你需要的业务修改、筛选或计算...
    # for item in data:
    #     item.score += 1.0

    # 直接将 Struct 列表编码成二进制 Msgpack
    packed_bytes = msgspec.msgpack.encode(data)

    # 写入目标 .msgpack 文件
    with open(target_msgpack, "wb") as f:
        f.write(packed_bytes)

    elapsed = time.time() - start_time
    
    return {
        "count": len(data),
        "source_size_mb": round(source_json.stat().st_size / (1024 * 1024), 2),
        "packed_size_mb": round(target_msgpack.stat().st_size / (1024 * 1024), 2),
        "elapsed_seconds": round(elapsed, 3)
    }
 