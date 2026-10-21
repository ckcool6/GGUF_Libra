# Copyright 2026 Lu ZhiYuan
# SPDX-License-Identifier: AGPL-3.0-only

import math
import re
from collections import Counter
from difflib import SequenceMatcher

def is_relative_duplicate(parent_text: str, child_text: str, threshold: float = 0.95) -> bool:
    """Determine if a child node is a near-duplicate of its parent (>= threshold similarity)."""
    p = parent_text.strip()
    c = child_text.strip()

    if p == c:
        return True

    lp, lc = len(p), len(c)
    if lp == 0 or lc == 0:
        return False

    # O(1) length pre-filter
    if abs(lp - lc) / max(lp, lc) > (1.0 - threshold):
        return False

    return SequenceMatcher(None, p, c).quick_ratio() >= threshold


def compute_shannon_entropy(text: str) -> float:
    """Calculate the Shannon information entropy of a given string in bits."""
    length = len(text)
    if length == 0:
        return 0.0

    counts = Counter(text)
    probs = (cnt / length for cnt in counts.values())
    
    entropy = -sum(p * math.log2(p) for p in probs)
    return entropy


def is_meaningless_content(text: str, min_entropy: float = 1.2, min_len: int = 6) -> bool:
    """
    Detect whether a node contains meaningless junk:
      1. Low character diversity: Unique-character-to-length ratio < 0.25.
      2. Low entropy (< min_entropy): Monotonous repetition (e.g., "ｗｗｗｗ", "666666").
      3. High symbol noise: Excessive punctuation/symbols from keyboard mashing (e.g., "x8#f!L0@zP^9...").
    """
    t = text.strip()
    length = len(t)

    # Rule 1: Empty or whitespace-only
    if length == 0:
        return True
    
    # Rule 2: Pure single-character repetition (e.g., "??", "111")
    if len(set(t)) == 1 and length >= 2:
        return True

    # Rule 3: Evaluation for medium-to-long text
    if length >= min_len:
        # Check character diversity ratio (repetitive looping)
        if (len(set(t)) / length) < 0.25:
            return True

        # Check Shannon entropy (monotonous spam)
        entropy = compute_shannon_entropy(t)
        if entropy < min_entropy:
            return True

        # Check keyboard mashing and chaotic symbol noise
        # Count non-alphanumeric, non-CJK (Chinese/Japanese/Korean) characters
        symbol_count = len(re.findall(r'[^a-zA-Z0-9\u4e00-\u9fa5\u3040-\u30ff]', t))
        if symbol_count / length > 0.35:  # Trigger if special symbols exceed 35%
            return True

    return False

## test runner
if __name__ == "__main__":
    # ==========================================================================
    # Test Stage 1: Relative Duplicate 
    # ==========================================================================
    p1 = "Rustを使って高性能なネットワークサービスを構築するにはどうすればいいですか？"
    c1 = "Rustを使って高性能なネットワークサービスを構築するにはどうすればいいですか？"
    c2 = "Rustを使って高性能なネットワークサービスを構築するにはどうすればいいですかね？"
    c3 = "今日はとてもいい天気ですね。"
    print("Exact duplicate :", is_relative_duplicate(p1, c1))       # Expected: True
    print("95% duplicate   :", is_relative_duplicate(p1, c2))       # Expected: True
    print("Normal distinct :", is_relative_duplicate(p1, c3))       # Expected: False

    # ==========================================================================
    # Test Stage 2: Entropy Junk (Low & High)
    # ==========================================================================
    # 1. Low entropy spam
    j1 = "ｗｗｗｗｗｗｗｗ"
    j2 = "ああああああああ"
    
    # 2. High entropy random noise / keyboard spam (Length >= 24)
    j_high = "x8#f!L0@zP^9&kQ2*mN4(vC6)"

    # 3. Valid normal content
    j3 = "ラプラシアン行列の固有値計算に関する、非常に有意义な質問ですね。"

    print("Low-Entropy Junk 1 :", is_meaningless_content(j1))      # Expected: True
    print("Low-Entropy Junk 2 :", is_meaningless_content(j2))      # Expected: True
    print("High-Entropy Junk  :", is_meaningless_content(j_high))  # Expected: True
    print("Valid Content      :", is_meaningless_content(j3))      # Expected: False