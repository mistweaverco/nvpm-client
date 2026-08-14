local parser = os.getenv("NVPM_TS_PARSER_PATH") or ""
local lang = os.getenv("NVPM_TS_LANG") or ""
local query_path = os.getenv("NVPM_TS_QUERY_PATH") or ""

local f, err = io.open(query_path, "rb")
if not f then
  io.stderr:write(tostring(err) .. "\n")
  vim.cmd("cquit 1")
end
local source = f:read("*a")
f:close()

if parser ~= "" then
  local ok, adderr = pcall(vim.treesitter.language.add, lang, { path = parser })
  if not ok then
    io.stderr:write(tostring(adderr) .. "\n")
    vim.cmd("cquit 1")
  end
end

local ok, perr = pcall(vim.treesitter.query.parse, lang, source)
if not ok then
  io.stderr:write(tostring(perr) .. "\n")
  vim.cmd("cquit 1")
end
