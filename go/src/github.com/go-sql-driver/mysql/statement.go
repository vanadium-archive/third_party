// Go MySQL Driver - A MySQL-Driver for Go's database/sql package
//
// Copyright 2012 The Go-MySQL-Driver Authors. All rights reserved.
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this file,
// You can obtain one at http://mozilla.org/MPL/2.0/.

package mysql

import (
	"database/sql/driver"
)

type mysqlStmt struct {
	mc         *mysqlConn
	id         uint32
	paramCount int
	columns    []mysqlField // cached from the first query
}

func (stmt *mysqlStmt) Close() error {
	if stmt.mc == nil || stmt.mc.netConn == nil {
		errLog.Print(ErrInvalidConn)
		return driver.ErrBadConn
	}

	err := stmt.mc.writeCommandPacketUint32(comStmtClose, stmt.id)
	stmt.mc = nil
	return err
}

func (stmt *mysqlStmt) NumInput() int {
	return stmt.paramCount
}

func (stmt *mysqlStmt) Exec(args []driver.Value) (driver.Result, error) {
	if stmt.mc.netConn == nil {
		errLog.Print(ErrInvalidConn)
		return nil, driver.ErrBadConn
	}
	// Send command
	errLog.Print("start: stmt.writeExecutePacket")
	err := stmt.writeExecutePacket(args)
	errLog.Print("end: stmt.writeExecutePacket")
	if err != nil {
		return nil, err
	}

	mc := stmt.mc

	mc.affectedRows = 0
	mc.insertId = 0

	// Read Result
	errLog.Print("start: mc.readResultSetHeaderPacket")
	resLen, err := mc.readResultSetHeaderPacket()
	errLog.Print("end: mc.readResultSetHeaderPacket")
	if err == nil {
		if resLen > 0 {
			// Columns
			errLog.Print("start: mc.readUntilEOF columns")
			err = mc.readUntilEOF()
			errLog.Print("end: mc.readUntilEOF columns")
			if err != nil {
				return nil, err
			}

			// Rows
			errLog.Print("start: mc.readUntilEOF rows")
			err = mc.readUntilEOF()
			errLog.Print("end: mc.readUntilEOF rows")
		}
		if err == nil {
			return &mysqlResult{
				affectedRows: int64(mc.affectedRows),
				insertId:     int64(mc.insertId),
			}, nil
		}
	}

	return nil, err
}

func (stmt *mysqlStmt) Query(args []driver.Value) (driver.Rows, error) {
	if stmt.mc.netConn == nil {
		errLog.Print(ErrInvalidConn)
		return nil, driver.ErrBadConn
	}
	// Send command
	err := stmt.writeExecutePacket(args)
	if err != nil {
		return nil, err
	}

	mc := stmt.mc

	// Read Result
	resLen, err := mc.readResultSetHeaderPacket()
	if err != nil {
		return nil, err
	}

	rows := new(binaryRows)
	rows.mc = mc

	if resLen > 0 {
		// Columns
		// If not cached, read them and cache them
		if stmt.columns == nil {
			rows.columns, err = mc.readColumns(resLen)
			stmt.columns = rows.columns
		} else {
			rows.columns = stmt.columns
			err = mc.readUntilEOF()
		}
	}

	return rows, err
}
