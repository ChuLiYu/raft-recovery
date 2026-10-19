# 文件導覽 / Documentation

本目錄收納技術文件、交付盤點與設計歷史。啟動指令與目前支援範圍請先看 [專案 README](../README.zh-CN.md)。

Start with the [project README](../README.md) for build and cluster commands, then read the Phase 3 delivery audit for verified behavior and limitations.

## 目前實作 / Current implementation

- [OrbStack 操作與環境管理](guides/ORBSTACK.zh-CN.md)：用單一腳本建置、執行、測試與清理；[遷移規格](planning/ORBSTACK_MIGRATION_PLAN.zh-CN.md)、[驗收報告](reports/ORBSTACK_VALIDATION.zh-CN.md)。
- [Phase 3 交付盤點](reports/PHASE3_DELIVERY.zh-CN.md) · [English](reports/PHASE3_DELIVERY.md)：功能、驗證結果、重要取捨與待補功能。
- [技術理解與面試準備（僅中文版）](guides/INTERVIEW_GUIDE.zh-CN.md)：Raft 安全性、請求生命週期、設計決策與專案表達。

## 文件分類 / Categories

| 目錄 | 內容 |
| --- | --- |
| [guides/](guides/) | 使用、開發與學習指南 |
| [architecture/](architecture/) | 架構圖、WAL 與快照原理 |
| [reports/](reports/) | 交付盤點、測試與除錯報告 |
| [phases/](phases/) | Phase 1–3 原始階段設計 |
| [planning/](planning/) | 實作順序、遷移與各階段計畫 |
| [design/](design/) | 程式碼審查與競態分析 |
| [development/](development/) | 開發紀錄與實作筆記 |

## 閱讀順序 / Reading order

1. [README](../README.zh-CN.md)：建置、三節點叢集與故障示範。
2. [Phase 3 交付盤點](reports/PHASE3_DELIVERY.zh-CN.md)：目前行為與限制的依據。
3. [中文文件索引](DOCS_INDEX.zh-CN.md) / [English index](DOCS_INDEX.md)：依主題查找文件。

階段規格、舊計畫與早期報告保留作為設計歷史；其中的目標、命令與測試數字不代表目前交付狀態。舊使用指南若與根目錄 README 不一致，以根目錄 README 為準。

Phase specifications, old plans and earlier reports are historical records. Their targets and test figures are not current delivery claims; use the root README for current commands.

文件語言慣例見 [LANGUAGE](LANGUAGE.zh-CN.md)。面試筆記僅保留中文版。
