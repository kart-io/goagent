// Package managementtest 提供管理子树的离线测试替身；存储实现来自 memstore。
package managementtest

import "github.com/kart-io/goagent/management/memstore"

type Store = memstore.Store

func NewStore() *Store { return memstore.New() }
