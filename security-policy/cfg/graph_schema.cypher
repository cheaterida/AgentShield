// Neo4j / RedisGraph 可用的 CFG 节点关系占位（Cypher 方言因后端而异，迁移时调整）。
// Node: BasicBlock { id, module_id, hash }
// Rel: FLOWS_TO { kind: "jump"|"fallthrough" }

CREATE CONSTRAINT block_id IF NOT EXISTS FOR (b:BasicBlock) REQUIRE b.id IS UNIQUE;
