package deadcode

import (
	"strings"

	"github.com/anatolykoptev/vaelor/internal/parser"
)

// pythonDunderMethods are Python magic methods called implicitly by the runtime.
var pythonDunderMethods = map[string]bool{
	"__str__": true, "__repr__": true, "__len__": true,
	"__getattr__": true, "__setattr__": true, "__delattr__": true, "__getattribute__": true,
	"__getitem__": true, "__setitem__": true, "__delitem__": true, "__contains__": true,
	"__enter__": true, "__exit__": true,
	"__aenter__": true, "__aexit__": true,
	"__iter__": true, "__next__": true, "__aiter__": true, "__anext__": true,
	"__call__": true, "__hash__": true, "__bool__": true,
	"__eq__": true, "__ne__": true, "__lt__": true, "__le__": true, "__gt__": true, "__ge__": true,
	"__add__": true, "__sub__": true, "__mul__": true, "__truediv__": true,
	"__floordiv__": true, "__mod__": true, "__pow__": true,
	"__radd__": true, "__rsub__": true, "__rmul__": true,
	"__iadd__": true, "__isub__": true, "__imul__": true,
	"__neg__": true, "__pos__": true, "__abs__": true, "__invert__": true,
	"__int__": true, "__float__": true, "__complex__": true, "__index__": true,
	"__format__": true, "__del__": true,
	"__new__": true, "__init_subclass__": true, "__class_getitem__": true,
	"__set_name__": true, "__get__": true, "__set__": true, "__delete__": true,
	"__missing__": true, "__reduce__": true, "__reduce_ex__": true,
	"__copy__": true, "__deepcopy__": true,
	"__await__": true, "__sizeof__": true,
}

// isPythonFrameworkEntryPoint returns true if a Python symbol has decorators
// indicating it is a framework entry point (route, fixture, property, etc.).
func isPythonFrameworkEntryPoint(sym *parser.Symbol) bool {
	if sym.Language != "python" {
		return false
	}
	for _, attr := range sym.Attributes {
		for _, prefix := range []string{
			"@app.route", "@router.", "@blueprint.",
			"@pytest.fixture", "@pytest.mark",
			"@property", "@staticmethod", "@classmethod", "@abstractmethod",
			"@override",
			"@click.command", "@click.group",
			"@celery.task", "@shared_task",
		} {
			if strings.HasPrefix(attr, prefix) {
				return true
			}
		}
	}
	return false
}
