package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRunPreservesArchivedProgramAndPrompt(t *testing.T) {
	// 只运行一次完整流程，避免为不同断言重复编译两个版本的程序。
	got, err := run(context.Background())
	require.NoError(t, err, "编译、归档、重开和执行应完整成功")
	require.Equal(t, "program-version=v1 prompt=旧版提示词", got.OldOutput, "旧修订必须执行旧程序并使用旧提示词")
	require.Equal(t, "program-version=v2 prompt=新版提示词", got.NewOutput, "新修订必须执行新程序并使用新提示词")
	require.Equal(t, "program-version=v1 prompt=旧版提示词", got.ReloadedOldOutput, "删除编译目录并重开归档后仍须恢复旧程序和旧提示词")
	require.NotEmpty(t, got.OldRevisionID, "旧修订必须具有内容标识")
	require.NotEmpty(t, got.NewRevisionID, "新修订必须具有内容标识")
	require.NotEqual(t, got.OldRevisionID, got.NewRevisionID, "程序和提示词变化必须产生不同修订")
}

func TestRunRejectsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := run(ctx)
	require.ErrorIs(t, err, context.Canceled, "预先取消的运行应直接返回取消错误")
}
