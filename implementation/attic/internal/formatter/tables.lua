-- Give HTML tables explicit paragraph widths so cells wrap on portrait pages.
-- Wide simple tables are continued horizontally, repeating the row label.
local function widths(t, rowLabel)
  t = t:walk({Str = function(word)
    if #word.text <= 12 or not word.text:find("[/_.+]") then return nil end
    local escaped = pandoc.write(pandoc.Pandoc({pandoc.Plain({word})}), 'latex'):gsub('%s+$', '')
    return pandoc.RawInline('latex', '\\atticword{' .. escaped .. '}')
  end})
  local n = #t.colspecs
  for i = 1, n do
    t.colspecs[i][2] = rowLabel and (i == 1 and 0.4 or 0.6 / (n - 1)) or 1 / n
  end
  return t
end

local function allrows(t)
  local rows = {}
  for _, r in ipairs(t.head.rows) do rows[#rows+1] = r end
  for _, body in ipairs(t.bodies) do
    for _, r in ipairs(body.head) do rows[#rows+1] = r end
    for _, r in ipairs(body.body) do rows[#rows+1] = r end
  end
  for _, r in ipairs(t.foot.rows) do rows[#rows+1] = r end
  return rows
end

function Table(t)
  local n = #t.colspecs
  if n == 0 then return t end
  if n <= 6 then return widths(t) end
  for _, row in ipairs(allrows(t)) do
    for _, cell in ipairs(row.cells) do
      if cell.col_span ~= 1 or cell.row_span ~= 1 then return widths(t) end
    end
  end
  local result = {}
  for start = 2, n, 4 do
    local part = t:clone()
    local indices = {1}
    for i = start, math.min(start + 3, n) do indices[#indices+1] = i end
    local specs = {}
    for _, i in ipairs(indices) do specs[#specs+1] = part.colspecs[i] end
    part.colspecs = specs
    for _, row in ipairs(allrows(part)) do
      local cells = {}
      for _, i in ipairs(indices) do cells[#cells+1] = row.cells[i] end
      row.cells = cells
    end
    result[#result+1] = widths(part, true)
  end
  return result
end

function Code(code)
  local escaped = pandoc.write(pandoc.Pandoc({pandoc.Plain({pandoc.Str(code.text)})}), 'latex'):gsub('%s+$', '')
  return pandoc.RawInline('latex', '\\atticcode{' .. escaped .. '}')
end

function Str(word)
  if #word.text <= 18 or not word.text:find("[/_.+]") then return nil end
  local escaped = pandoc.write(pandoc.Pandoc({pandoc.Plain({word})}), 'latex'):gsub('%s+$', '')
  return pandoc.RawInline('latex', '\\atticword{' .. escaped .. '}')
end
