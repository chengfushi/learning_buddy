import os
import sys

# 确保 agent/ 在 sys.path 中，使测试可 `import db` / `import rag` 等模块。
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
