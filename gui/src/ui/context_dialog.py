from PyQt6.QtWidgets import (
    QDialog, QVBoxLayout, QHBoxLayout, QLabel,
    QLineEdit, QPushButton, QFormLayout, QDialogButtonBox
)

class ContextDialog(QDialog):
    def __init__(self, parent=None, context_text=None):
        super().__init__(parent)
        self.setMinimumWidth(600)
        self.setMinimumHeight(200)
        self.setWindowTitle("添加上下文")
        if context_text:
            self.setWindowTitle("编辑上下文")

        # 创建主布局
        layout = QVBoxLayout(self)
        
        # 创建表单布局
        form_layout = QFormLayout()
        layout.addLayout(form_layout)  # 将表单布局添加到主布局中
        
        # 创建输入框
        self.name_input = QLineEdit()
        self.name_input.setMinimumWidth(400)
        self.name_input.setMinimumHeight(30)
        
        self.host_input = QLineEdit()
        self.host_input.setMinimumWidth(400)
        self.host_input.setMinimumHeight(30)
        
        self.port_input = QLineEdit()
        self.port_input.setMinimumWidth(400)
        self.port_input.setMinimumHeight(30)
        
        # 使用表单布局添加输入框
        form_layout.addRow("名称:", self.name_input)
        form_layout.addRow("主机:", self.host_input)
        form_layout.addRow("端口:", self.port_input)
        
        # 添加按钮到主布局
        button_box = QDialogButtonBox(
            QDialogButtonBox.StandardButton.Ok | 
            QDialogButtonBox.StandardButton.Cancel
        )
        button_box.accepted.connect(self.accept)
        button_box.rejected.connect(self.reject)
        layout.addWidget(button_box)

        # 如果有现有数据，填充到输入框
        if context_text:
            name = context_text.split(" (")[0]
            host_port = context_text.split(" (")[1].rstrip(")")
            host, port = host_port.split(":")
            self.name_input.setText(name)
            self.host_input.setText(host)
            self.port_input.setText(port)
        else:
            # 设置默认值
            self.port_input.setText("11211")

    def get_context_data(self):
        return {
            "name": self.name_input.text(),
            "host": self.host_input.text(),
            "port": self.port_input.text()
        }