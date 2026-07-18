package ai

// Ask-Data boards: a per-org collection of saved NL→SQL→ECharts charts. Separate from
// the widget/GridStack dashboards. A saved chart stores {question, sql, echart_option};
// the frontend re-runs the SQL (via RunSQL) on open for live data. All queries are
// org-scoped by the caller's JWT.

import (
	"context"
	"encoding/json"

	"iot-dashboard/internal/database"
	"iot-dashboard/internal/middleware"

	"github.com/gofiber/fiber/v2"
)

func orgOf(c *fiber.Ctx) (string, bool) {
	u := middleware.GetUser(c)
	if u == nil {
		return "", false
	}
	return u.OrgId, true
}

// GET /ai/boards
func ListBoards(c *fiber.Ctx) error {
	org, ok := orgOf(c)
	if !ok {
		return c.Status(401).JSON(fiber.Map{"success": false})
	}
	rows, err := database.Pool.Query(context.Background(),
		`SELECT b.id, b.name, b.updated_at, COUNT(ch.id)
		 FROM ai_boards b LEFT JOIN ai_board_charts ch ON ch.board_id = b.id
		 WHERE b.organization_id = $1
		 GROUP BY b.id ORDER BY b.updated_at DESC`, org)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": fiber.Map{"message": err.Error()}})
	}
	defer rows.Close()
	out := []fiber.Map{}
	for rows.Next() {
		var id, name string
		var updated any
		var count int
		if err := rows.Scan(&id, &name, &updated, &count); err != nil {
			return c.Status(500).JSON(fiber.Map{"success": false, "error": fiber.Map{"message": err.Error()}})
		}
		out = append(out, fiber.Map{"id": id, "name": name, "updatedAt": updated, "chartCount": count})
	}
	return c.JSON(fiber.Map{"success": true, "data": out})
}

// POST /ai/boards {name}
func CreateBoard(c *fiber.Ctx) error {
	u := middleware.GetUser(c)
	if u == nil {
		return c.Status(401).JSON(fiber.Map{"success": false})
	}
	var body struct {
		Name string `json:"name"`
	}
	if err := c.BodyParser(&body); err != nil || body.Name == "" {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": fiber.Map{"message": "name is required"}})
	}
	var id string
	err := database.Pool.QueryRow(context.Background(),
		`INSERT INTO ai_boards (organization_id, user_id, name) VALUES ($1, $2, $3) RETURNING id`,
		u.OrgId, u.Sub, body.Name).Scan(&id)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": fiber.Map{"message": err.Error()}})
	}
	return c.Status(201).JSON(fiber.Map{"success": true, "data": fiber.Map{"id": id, "name": body.Name}})
}

// GET /ai/boards/:id → board + charts
func GetBoard(c *fiber.Ctx) error {
	org, ok := orgOf(c)
	if !ok {
		return c.Status(401).JSON(fiber.Map{"success": false})
	}
	id := c.Params("id")
	var name string
	if err := database.Pool.QueryRow(context.Background(),
		`SELECT name FROM ai_boards WHERE id = $1 AND organization_id = $2`, id, org).Scan(&name); err != nil {
		return c.Status(404).JSON(fiber.Map{"success": false, "error": fiber.Map{"message": "board not found"}})
	}
	rows, err := database.Pool.Query(context.Background(),
		`SELECT id, question, sql, echart_option FROM ai_board_charts WHERE board_id = $1 ORDER BY "order", created_at`, id)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": fiber.Map{"message": err.Error()}})
	}
	defer rows.Close()
	charts := []fiber.Map{}
	for rows.Next() {
		var cid, question, sqlText string
		var option json.RawMessage
		if err := rows.Scan(&cid, &question, &sqlText, &option); err != nil {
			return c.Status(500).JSON(fiber.Map{"success": false, "error": fiber.Map{"message": err.Error()}})
		}
		charts = append(charts, fiber.Map{"id": cid, "question": question, "sql": sqlText, "echartOption": option})
	}
	return c.JSON(fiber.Map{"success": true, "data": fiber.Map{"id": id, "name": name, "charts": charts}})
}

// PATCH /ai/boards/:id {name}
func RenameBoard(c *fiber.Ctx) error {
	org, ok := orgOf(c)
	if !ok {
		return c.Status(401).JSON(fiber.Map{"success": false})
	}
	var body struct {
		Name string `json:"name"`
	}
	if err := c.BodyParser(&body); err != nil || body.Name == "" {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": fiber.Map{"message": "name is required"}})
	}
	ct, err := database.Pool.Exec(context.Background(),
		`UPDATE ai_boards SET name = $1, updated_at = NOW() WHERE id = $2 AND organization_id = $3`,
		body.Name, c.Params("id"), org)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": fiber.Map{"message": err.Error()}})
	}
	if ct.RowsAffected() == 0 {
		return c.Status(404).JSON(fiber.Map{"success": false, "error": fiber.Map{"message": "board not found"}})
	}
	return c.JSON(fiber.Map{"success": true, "data": fiber.Map{"id": c.Params("id"), "name": body.Name}})
}

// DELETE /ai/boards/:id
func DeleteBoard(c *fiber.Ctx) error {
	org, ok := orgOf(c)
	if !ok {
		return c.Status(401).JSON(fiber.Map{"success": false})
	}
	ct, err := database.Pool.Exec(context.Background(),
		`DELETE FROM ai_boards WHERE id = $1 AND organization_id = $2`, c.Params("id"), org)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": fiber.Map{"message": err.Error()}})
	}
	if ct.RowsAffected() == 0 {
		return c.Status(404).JSON(fiber.Map{"success": false, "error": fiber.Map{"message": "board not found"}})
	}
	return c.JSON(fiber.Map{"success": true})
}

// POST /ai/boards/:id/charts {question, sql, echartOption}
func AddBoardChart(c *fiber.Ctx) error {
	org, ok := orgOf(c)
	if !ok {
		return c.Status(401).JSON(fiber.Map{"success": false})
	}
	boardID := c.Params("id")
	// Verify the board belongs to the caller's org before writing.
	var exists bool
	if err := database.Pool.QueryRow(context.Background(),
		`SELECT EXISTS(SELECT 1 FROM ai_boards WHERE id = $1 AND organization_id = $2)`, boardID, org).Scan(&exists); err != nil || !exists {
		return c.Status(404).JSON(fiber.Map{"success": false, "error": fiber.Map{"message": "board not found"}})
	}
	var body struct {
		Question     string          `json:"question"`
		SQL          string          `json:"sql"`
		EchartOption json.RawMessage `json:"echartOption"`
	}
	if err := c.BodyParser(&body); err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": fiber.Map{"message": "invalid body"}})
	}
	sqlText, err := validateSQL(body.SQL)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": false, "error": fiber.Map{"message": "sql rejected: " + err.Error()}})
	}
	if len(body.EchartOption) == 0 {
		body.EchartOption = json.RawMessage("{}")
	}
	var id string
	err = database.Pool.QueryRow(context.Background(),
		`INSERT INTO ai_board_charts (board_id, question, sql, echart_option, "order")
		 VALUES ($1, $2, $3, $4, (SELECT COALESCE(MAX("order")+1, 0) FROM ai_board_charts WHERE board_id = $1))
		 RETURNING id`,
		boardID, body.Question, sqlText, body.EchartOption).Scan(&id)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": fiber.Map{"message": err.Error()}})
	}
	_, _ = database.Pool.Exec(context.Background(), `UPDATE ai_boards SET updated_at = NOW() WHERE id = $1`, boardID)
	return c.Status(201).JSON(fiber.Map{"success": true, "data": fiber.Map{"id": id}})
}

// DELETE /ai/boards/:id/charts/:chartId
func DeleteBoardChart(c *fiber.Ctx) error {
	org, ok := orgOf(c)
	if !ok {
		return c.Status(401).JSON(fiber.Map{"success": false})
	}
	ct, err := database.Pool.Exec(context.Background(),
		`DELETE FROM ai_board_charts ch USING ai_boards b
		 WHERE ch.id = $1 AND ch.board_id = b.id AND b.id = $2 AND b.organization_id = $3`,
		c.Params("chartId"), c.Params("id"), org)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": false, "error": fiber.Map{"message": err.Error()}})
	}
	if ct.RowsAffected() == 0 {
		return c.Status(404).JSON(fiber.Map{"success": false, "error": fiber.Map{"message": "chart not found"}})
	}
	return c.JSON(fiber.Map{"success": true})
}
