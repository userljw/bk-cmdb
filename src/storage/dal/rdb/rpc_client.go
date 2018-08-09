/*
 * Tencent is pleased to support the open source community by making 蓝鲸 available.
 * Copyright (C) 2017-2018 THL A29 Limited, a Tencent company. All rights reserved.
 * Licensed under the MIT License (the "License"); you may not use this file except
 * in compliance with the License. You may obtain a copy of the License at
 * http://opensource.org/licenses/MIT
 * Unless required by applicable law or agreed to in writing, software distributed under
 * the License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND,
 * either express or implied. See the License for the specific language governing permissions and
 * limitations under the License.
 */

package rdb

import (
	"configcenter/src/storage/dal"
	"configcenter/src/storage/rpc"
	"configcenter/src/storage/types"
	"context"
	"errors"
	"fmt"
	"strconv"
)

// RDB implement client.DALRDB interface
type RDB struct {
	RequestID string // 请求ID,可选项
	Processor string // 处理进程号，结构为"IP:PORT-PID"用于识别事务session被存于那个TM多活实例
	TxnID     string // 事务ID,uuid
	rpc       *rpc.Client
	getServer types.GetServerFunc
}

var _ dal.RDB = new(RDB)
var _ dal.RDBTxn = new(RDBTxn)

func ConnectRDBWithDiscover(getServer types.GetServerFunc) (*RDB, error) {
	servers, err := getServer()
	if err != nil {
		return nil, err
	}

	rpccli, err := rpc.DialHTTPPath("tcp", servers[0], "/txn/v3/rpc")
	if err != nil {
		return nil, err
	}
	return &RDB{
		rpc:       rpccli,
		getServer: getServer,
	}, nil
}

// ConnectRDB returns new RDB
func ConnectRDB(uri string) (*RDB, error) {
	rpccli, err := rpc.DialHTTPPath("tcp", uri, "/txn/v3/rpc")
	if err != nil {
		return nil, err
	}
	return &RDB{
		rpc: rpccli,
	}, nil
}

func (c *RDB) clone() *RDB {
	nc := RDB{
		RequestID: c.RequestID,
		Processor: c.Processor,
		TxnID:     c.TxnID,
		rpc:       c.rpc,
	}
	return &nc
}

// Collection collection operation
func (c *RDB) Collection(collection string) dal.Collection {
	col := Collection{}
	col.RequestID = c.RequestID
	col.Processor = c.Processor
	col.TxnID = c.TxnID
	col.collection = collection
	col.rpc = c.rpc

	return &col
}

// Collection implement client.Collection interface
type Collection struct {
	RequestID  string // 请求ID,可选项
	Processor  string // 处理进程号，结构为"IP:PORT-PID"用于识别事务session被存于那个TM多活实例
	TxnID      string // 事务ID,uuid
	collection string // 集合名
	rpc        *rpc.Client
}

// Find 查询多个并反序列化到 Result
func (c *Collection) Find(ctx context.Context, filter types.Filter) dal.Find {
	msg := types.OPFIND{}
	msg.OPCode = types.OPFind
	msg.RequestID = c.RequestID
	msg.TxnID = c.TxnID
	msg.Collection = c.collection
	msg.Selector.Encode(filter)

	return &Find{Collection: c, msg: &msg}
}

// Find define a find operation
type Find struct {
	*Collection
	msg *types.OPFIND
}

// Fields 查询字段
func (f *Find) Fields(fields ...string) dal.Find {
	projection := types.Document{}
	for _, field := range fields {
		projection[field] = true
	}
	f.msg.Projection = projection
	return f
}

// Sort 查询排序
func (f *Find) Sort(sort string) dal.Find {
	f.msg.Sort = sort
	return f
}

// Start 查询上标
func (f *Find) Start(start uint64) dal.Find {
	f.msg.Start = start
	return f
}

// Limit 查询限制
func (f *Find) Limit(limit uint64) dal.Find {
	f.msg.Limit = limit
	return f
}

// All 查询多个
func (f *Find) All(result interface{}) error {
	reply := types.OPREPLY{}
	err := f.rpc.Call(types.CommandDBOperation, f.msg, &reply)
	if err != nil {
		return err
	}
	if !reply.OK {
		return errors.New(reply.Message)
	}
	return reply.Docs.Decode(result)
}

// One 查询一个
func (f *Find) One(result interface{}) error {
	reply := types.OPREPLY{}
	err := f.rpc.Call(types.CommandDBOperation, f.msg, &reply)
	if err != nil {
		return err
	}
	if !reply.OK {
		return errors.New(reply.Message)
	}

	if len(reply.Docs[0]) <= 0 {
		return dal.ErrDocumentNotFound
	}
	return reply.Docs[0].Decode(result)
}

// Insert 插入数据, docs 可以为 单个数据 或者 多个数据
func (c *Collection) Insert(ctx context.Context, docs interface{}) error {
	msg := types.OPINSERT{}
	msg.OPCode = types.OPInsert
	msg.RequestID = c.RequestID
	msg.TxnID = c.TxnID
	msg.Collection = c.collection

	if err := msg.DOCS.Encode(docs); err != nil {
		return err
	}

	reply := types.OPREPLY{}
	err := c.rpc.Call(types.CommandDBOperation, &msg, &reply)
	if err != nil {
		return err
	}
	if !reply.OK {
		return errors.New(reply.Message)
	}
	return nil
}

// Update 更新数据
func (c *Collection) Update(ctx context.Context, filter types.Filter, doc interface{}) error {
	msg := types.OPUPDATE{}
	msg.OPCode = types.OPUpdate
	msg.RequestID = c.RequestID
	msg.TxnID = c.TxnID
	msg.Collection = c.collection
	if err := msg.DOC.Encode(types.Document{
		"$set": doc,
	}); err != nil {
		return err
	}
	if err := msg.Selector.Encode(filter); err != nil {
		return err
	}

	reply := types.OPREPLY{}
	err := c.rpc.Call(types.CommandDBOperation, &msg, &reply)
	if err != nil {
		return err
	}
	if !reply.OK {
		return errors.New(reply.Message)
	}
	return nil
}

// Delete 删除数据
func (c *Collection) Delete(ctx context.Context, filter types.Filter) error {
	msg := types.OPDELETE{}
	msg.OPCode = types.OPDelete
	msg.RequestID = c.RequestID
	msg.TxnID = c.TxnID
	msg.Collection = c.collection
	if err := msg.Selector.Encode(filter); err != nil {
		return err
	}

	reply := types.OPREPLY{}
	err := c.rpc.Call(types.CommandDBOperation, &msg, &reply)
	if err != nil {
		return err
	}
	if !reply.OK {
		return errors.New(reply.Message)
	}
	return nil
}

// Count 统计数量(非事务)
func (c *Collection) Count(ctx context.Context, filter types.Filter) (uint64, error) {
	msg := types.OPCOUNT{}
	msg.OPCode = types.OPCount
	msg.RequestID = c.RequestID
	// msg.TxnID = c.TxnID // because Count was not supported for transaction in mongo
	msg.Collection = c.collection
	if err := msg.Selector.Encode(filter); err != nil {
		return 0, err
	}

	reply := types.OPREPLY{}
	err := c.rpc.Call(types.CommandDBOperation, &msg, &reply)
	if err != nil {
		return 0, err
	}
	if !reply.OK {
		return 0, errors.New(reply.Message)
	}
	return reply.Count, nil
}

// NextSequence 获取新序列号(非事务)
func (c *RDB) NextSequence(ctx context.Context, sequenceName string) (uint64, error) {
	msg := types.OPFINDANDMODIFY{}
	msg.OPCode = types.OPFindAndModify
	msg.RequestID = c.RequestID
	msg.Collection = sequenceName
	if err := msg.DOC.Encode(types.Document{
		"$inc": types.Document{"SequenceID": 1},
	}); err != nil {
		return 0, err
	}
	if err := msg.Selector.Encode(types.Document{
		"_id": sequenceName,
	}); err != nil {
		return 0, err
	}

	msg.Upsert = true
	msg.ReturnNew = true

	reply := types.OPREPLY{}
	err := c.rpc.Call(types.CommandDBOperation, &msg, &reply)
	if err != nil {
		return 0, err
	}
	if !reply.OK {
		return 0, errors.New(reply.Message)
	}

	if len(reply.Docs) <= 0 {
		return 0, dal.ErrDocumentNotFound
	}

	return strconv.ParseUint(fmt.Sprint(reply.Docs[0]["SequenceID"]), 10, 64)
}

// StartTransaction 开启新事务
func (c *RDB) StartTransaction(ctx context.Context, opt dal.JoinOption) (dal.RDBTxn, error) {
	msg := types.OPSTARTTTRANSATION{}
	msg.OPCode = types.OPStartTransaction
	msg.RequestID = c.RequestID

	reply := types.OPREPLY{}
	err := c.rpc.Call(types.CommandDBOperation, &msg, &reply)
	if err != nil {
		return nil, err
	}
	if !reply.OK {
		return nil, errors.New(reply.Message)
	}

	nc := new(RDBTxn)
	nc.RDB = c.clone()
	nc.TxnID = reply.TxnID
	nc.Processor = reply.Processor
	nc.RequestID = opt.RequestID
	return nc, nil
}

// JoinTransaction 加入事务, controller 加入某个事务
func (c *RDB) JoinTransaction(opt dal.JoinOption) dal.RDBTxn {
	nc := new(RDBTxn)
	nc.RDB = c.clone()
	nc.TxnID = opt.TxnID
	nc.RequestID = opt.RequestID
	nc.Processor = opt.Processor
	return nc
}

// Ping 健康检查
func (c *RDB) Ping() error {
	return nil
}

// RDBTxn implement dal.RDBTxn
type RDBTxn struct {
	*RDB
}

func (c *RDBTxn) clone() *RDBTxn {
	nc := RDBTxn{}
	nc.RDB = c.RDB.clone()
	nc.RequestID = c.RequestID
	nc.Processor = c.Processor
	nc.TxnID = c.TxnID
	nc.rpc = c.rpc
	return &nc
}

// Commit 提交事务
func (c *RDBTxn) Commit() error {
	msg := types.OPCOMMIT{}
	msg.OPCode = types.OPCommit
	msg.RequestID = c.RequestID
	msg.TxnID = c.TxnID

	reply := types.OPREPLY{}
	err := c.rpc.Call(types.CommandDBOperation, &msg, &reply)
	if err != nil {
		return err
	}
	if !reply.OK {
		return errors.New(reply.Message)
	}
	return nil

}

// Abort 取消事务
func (c *RDBTxn) Abort() error {
	msg := types.OPABORT{}
	msg.OPCode = types.OPAbort
	msg.RequestID = c.RequestID
	msg.TxnID = c.TxnID

	reply := types.OPREPLY{}
	err := c.rpc.Call(types.CommandDBOperation, &msg, &reply)
	if err != nil {
		return err
	}
	if !reply.OK {
		return errors.New(reply.Message)
	}
	return nil
}

// TxnInfo 当前事务信息，用于事务发起者往下传递
func (c *RDBTxn) TxnInfo() *types.Tansaction {
	return nil
}