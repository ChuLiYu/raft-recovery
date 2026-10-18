# Documentation Index

> Complete navigation guide for Beaver-Raft documentation

English | [Chinese version](DOCS_INDEX.zh-CN.md)

## 📚 Documentation Structure

### 🎯 Getting Started (Recommended Reading Order)

1. **[README.md](../README.md)** - Project overview & quick start
2. **[USAGE_GUIDE.md](guides/USAGE_GUIDE.md)** - User manual (commands, config, monitoring)
3. **[QUICKSTART.md](guides/QUICKSTART.md)** - Developer implementation guide

### 📋 Complete Reference

4. **[PHASE1_SUMMARY.md](reports/PHASE1_SUMMARY.md)** - Phase 1 feature summary
5. **[IMPLEMENTATION_ORDER.md](planning/IMPLEMENTATION_ORDER.md)** - Module implementation order & details
6. **[TEST_COVERAGE_REPORT.md](reports/TEST_COVERAGE_REPORT.md)** - Test coverage report

### 🏗️ Architecture & Design

7. **[docs/phase1-architecture.md](architecture/phase1-architecture.md)** - System architecture design
8. **[docs/ai-notes.md](development/ai-notes.md)** - AI design notes & decisions
9. **[docs/phase1-snapshot-aware-job-queue.md](phases/phase1-snapshot-aware-job-queue.md)** - Phase 1 technical deep dive

### 🔮 Future Planning

10. **[Project roadmap](../roadmap.md)** - Project roadmap
11. **[Phase 2 design](phases/phase2-falconqueue.md)** - Phase 2 design history
12. **[Phase 3 design](phases/phase3-beaver-raft.md)** - Phase 3 design history

### ✅ Phase 3: Delivered behavior and study

- **[Phase 3 delivery audit](PHASE3_DELIVERY.md)** ([中文](PHASE3_DELIVERY.zh-CN.md)) - Current implementation, verification, and known limitations
- **[Technical and interview guide](INTERVIEW_GUIDE.md)** ([中文](INTERVIEW_GUIDE.zh-CN.md)) - Architecture, correctness arguments, and discussion prompts
- **[Original implementation plan](planning/PHASE3_IMPLEMENTATION_PLAN.md)** - Historical plan; consult the delivery audit for current status

## 🎓 Documentation by Use Case

### Scenario 1: I Want to Use the System Quickly

Reading order:

1. [README.md](../README.md) - Understand what it is
2. [USAGE_GUIDE.md](guides/USAGE_GUIDE.md) - Learn how to use
3. Run `make demo` - See it in action

**Time**: 15 minutes

### Scenario 2: I Want to Develop or Modify Code

Reading order:

1. [QUICKSTART.md](guides/QUICKSTART.md) - Development environment setup
2. [IMPLEMENTATION_ORDER.md](planning/IMPLEMENTATION_ORDER.md) - Understand module structure
3. [docs/phase1-architecture.md](architecture/phase1-architecture.md) - Understand design
4. Review pseudocode comments in source files

**Time**: 1-2 hours

### Scenario 3: I Want Deep System Understanding

Reading order:

1. [docs/ai-notes.md](development/ai-notes.md) - Design decisions
2. [docs/phase1-architecture.md](architecture/phase1-architecture.md) - Architecture design
3. [docs/phase1-snapshot-aware-job-queue.md](phases/phase1-snapshot-aware-job-queue.md) - Technical details
4. [PHASE1_SUMMARY.md](reports/PHASE1_SUMMARY.md) - Complete summary

**Time**: 2-3 hours

### Scenario 4: I Want to Check Test Status

Reading order:

1. [TEST_COVERAGE_REPORT.md](reports/TEST_COVERAGE_REPORT.md) - Test report
2. Review `internal/*/*_test.go` - Specific test cases
3. Check `test/integration/` - Integration tests

**Time**: 30 minutes

## 📖 Document Function Reference

| Document | Purpose | Target Audience | Reading Time |
|----------|---------|-----------------|--------------|
| **README.md** | Project intro & quick start | Everyone | 5 min |
| **USAGE_GUIDE.md** | User manual & command reference | Users, DevOps | 10 min |
| **QUICKSTART.md** | Development guide & impl details | Developers | 30 min |
| **PHASE1_SUMMARY.md** | Feature summary & tech stack | Technical staff | 15 min |
| **IMPLEMENTATION_ORDER.md** | Implementation order & modules | Developers | 1 hour |
| **TEST_COVERAGE_REPORT.md** | Test coverage & quality | QA, Developers | 10 min |
| **docs/phase1-architecture.md** | Architecture & system principles | Architects, Developers | 1 hour |
| **docs/ai-notes.md** | Design thinking & tradeoffs | Architects | 30 min |
| **docs/roadmap.md** | Future planning & evolution | PM, Architects | 15 min |

## 🔍 Quick Find

### I want to know...

- **How to start the system?** → [USAGE_GUIDE.md](guides/USAGE_GUIDE.md#quick-start)
- **How to submit jobs?** → [USAGE_GUIDE.md](guides/USAGE_GUIDE.md#create-job-files)
- **How to test crash recovery?** → [USAGE_GUIDE.md](guides/USAGE_GUIDE.md#test-crash-recovery)
- **What is the architecture?** → [docs/phase1-architecture.md](architecture/phase1-architecture.md)
- **How to implement a module?** → [IMPLEMENTATION_ORDER.md](planning/IMPLEMENTATION_ORDER.md)
- **How does WAL work?** → [docs/phase1-snapshot-aware-job-queue.md](phases/phase1-snapshot-aware-job-queue.md)
- **Why this design?** → [docs/ai-notes.md](development/ai-notes.md)
- **Test coverage status?** → [TEST_COVERAGE_REPORT.md](reports/TEST_COVERAGE_REPORT.md)
- **Future features?** → [docs/roadmap.md](../roadmap.md)

## 📦 Document Update History

- **2025-11-01**: Created documentation index, bilingual support
- **2025-10-31**: Completed all Phase 1 documents
- **2025-10-30**: Added test coverage report
- **2025-10-29**: Optimized README and usage guide

## 🤝 Documentation Contribution

Found doc issues or have improvement suggestions?

1. Report in Issues
2. Submit Pull Request
3. Contact maintainers

## 📝 Documentation Standards

- Use spaces between Chinese and English text
- Specify language for code blocks
- Use meaningful heading descriptions
- Provide runnable examples

## 🌐 Language Versions

All major documents are available in both English and Chinese:

- **English** (Primary): `FILENAME.md`
- **Chinese** (Secondary): `FILENAME.zh-CN.md`

See [LANGUAGE.md](LANGUAGE.md) for complete language guide.

---

**Quick Links**: [Usage Guide](guides/USAGE_GUIDE.md) | [Dev Guide](guides/QUICKSTART.md) | [Architecture](architecture/phase1-architecture.md)
