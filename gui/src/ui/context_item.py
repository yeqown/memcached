import random

from PyQt6.QtWidgets import QStyledItemDelegate, QStyle
from PyQt6.QtCore import Qt, QRect, QSize, QRectF, QPointF
from PyQt6.QtGui import QColor, QLinearGradient, QPainter, QFont, QPainterPath, QIcon


class ContextItemDelegate(QStyledItemDelegate):
    handle_delete_context = None
    handle_add_context = None


    def __init__(self, parent=None, handle_delete_context=None, handle_add_context=None, handle_edit_context=None):
        super().__init__(parent)
        self.default_colors = ((220, 220, 220), (200, 200, 200))
        self.add_icon = QIcon("src/ui/assets/add.png")
        self.delete_icon = QIcon("src/ui/assets/trash.png")
        self.edit_icon = QIcon("src/ui/assets/edit.png")    # 新增编辑图标
        self.icon_size = QSize(24, 24)

        self.handle_delete_context = handle_delete_context
        self.handle_add_context = handle_add_context
        self.handle_edit_context = handle_edit_context      # 新增编辑处理函数

    def paint(self, painter, option, index):
        painter.save()
        
        # 特殊处理添加按钮项
        if index.data() == "+":
            self.paint_add_button(painter, option)
            painter.restore()
            return

        # 正常项目的绘制
        rect = option.rect
        
        # 统一使用灰色背景
        gradient = QLinearGradient(
            QPointF(rect.topLeft()),
            QPointF(rect.bottomRight())
        )
        gradient.setColorAt(0, QColor(245, 245, 245))
        gradient.setColorAt(1, QColor(235, 235, 235))
        painter.fillRect(rect, gradient)
        
        # 获取数据
        data = index.data()
        name = data.split(" (")[0]
        host_port = data.split(" (")[1].rstrip(")")
        
        # 绘制内容
        content_rect = QRect(rect)
        content_rect.adjust(10, 10, -40, -10)
        
        # 绘制名称（居中对齐，加大字号）
        name_font = QFont()
        name_font.setPointSize(18)  # 原来是 12，增加 1.5 倍
        name_font.setBold(True)
        painter.setFont(name_font)
        painter.setPen(QColor(0, 0, 0))  # 黑色文字

        name_rect = QRect(content_rect)
        name_rect.setHeight(40)  # 增加高度以适应更大的字体
        painter.drawText(name_rect, Qt.AlignmentFlag.AlignCenter, name)

        # 绘制服务器信息（左对齐，加大字号）
        info_font = QFont()
        info_font.setPointSize(12) 
        painter.setFont(info_font)
        
        info_rect = QRect(content_rect)
        info_rect.setTop(name_rect.bottom() + 5)
        painter.drawText(info_rect, Qt.AlignmentFlag.AlignLeft, f"服务器: {host_port}")
        
        # # 绘制服务器数量（左对齐）
        # count_rect = QRect(content_rect)
        # count_rect.setTop(info_rect.top() + 25)  # 稍微增加间距
        # painter.drawText(count_rect, Qt.AlignmentFlag.AlignLeft, "Servers: 1")
        
        # 如果被选中，添加选中效果
        if option.state & QStyle.StateFlag.State_Selected:
            painter.fillRect(rect, QColor(51, 153, 255, 50))
            
        # 绘制编辑按钮（在删除按钮左侧）
        edit_rect = QRect(
            rect.right() - 68,  # 34 * 2
            rect.center().y() - 12,
            24,
            24
        )
        self.edit_icon.paint(painter, edit_rect)

        # 绘制删除按钮
        delete_rect = QRect(
            rect.right() - 34,
            rect.center().y() - 12,
            24,
            24
        )
        self.delete_icon.paint(painter, delete_rect)

        # 绘制底部分割线
        painter.setPen(QColor(200, 200, 200))
        painter.drawLine(rect.left(), rect.bottom(), rect.right(), rect.bottom())

        painter.restore()

    def paint_add_button(self, painter, option):
        rect = option.rect
        # 绘制浅色背景
        painter.fillRect(rect, QColor(245, 245, 245))
        
        # 绘制加号图标
        icon_rect = QRect(
            rect.center().x() - 12,
            rect.center().y() - 12,
            24,
            24
        )
        self.add_icon.paint(painter, icon_rect)

    def editorEvent(self, event, model, option, index):
        if event.type() == event.Type.MouseButtonRelease:
            if index.data() == "+":
                self.handle_add_context()
                return True
            
            # 检查是否点击了编辑按钮
            edit_rect = QRect(
                option.rect.right() - 68,
                option.rect.center().y() - 12,
                24,
                24
            )
            if edit_rect.contains(event.pos()):
                self.handle_edit_context(index)
                return True
            
            # 检查是否点击了删除按钮
            delete_rect = QRect(
                option.rect.right() - 34,
                option.rect.center().y() - 12,
                24,
                24
            )
            if delete_rect.contains(event.pos()):
                self.handle_delete_context(index)
                return True
        
        return super().editorEvent(event, model, option, index)

    def sizeHint(self, option, index):
        return QSize(option.rect.width(), 100)